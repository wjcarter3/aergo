package raft

import (
	"encoding/json"
	"fmt"
	"github.com/aergoio/aergo/consensus"
	"github.com/aergoio/aergo/message"
	"github.com/aergoio/aergo/p2p"
	"github.com/aergoio/aergo/p2p/p2putil"
	"github.com/aergoio/aergo/pkg/component"
	"github.com/aergoio/aergo/types"
	"github.com/libp2p/go-libp2p-peer"
	"sync"
	"time"
)

// raft cluster membership
// copy from dpos/bp
// TODO refactoring
// Cluster represents a cluster of block producers.
type Cluster struct {
	component.ICompSyncRequester
	rs *raftServer
	sync.Mutex

	ID     uint64
	Size   uint16
	Member map[uint64]*blockProducer
	Index  map[peer.ID]uint64 // peer ID to raft ID mapping

	BPUrls []string //for raft server

	cdb consensus.ChainDB
}

type RaftInfo struct {
	Leader uint64
	Total  uint16
	RaftId uint64
	Status *json.RawMessage
}

type blockProducer struct {
	RaftID uint64
	Url    string
	PeerID peer.ID
}

func (bp *blockProducer) isDifferent(x *blockProducer) bool {
	if bp.RaftID == x.RaftID || bp.Url == x.Url || bp.PeerID == x.PeerID {
		return false
	}

	return true
}

func NewCluster(bf *BlockFactory, raftID uint64, size uint16) *Cluster {
	cl := &Cluster{
		ICompSyncRequester: bf,
		ID:                 raftID,
		Size:               size,
		Member:             make(map[uint64]*blockProducer),
		Index:              make(map[peer.ID]uint64),
		BPUrls:             make([]string, size),
		cdb:                bf.ChainDB,
	}

	return cl
}

func (cl *Cluster) Quorum() uint16 {
	return cl.Size/2 + 1
}

func (cc *Cluster) addMember(id uint64, url string, peerID peer.ID) error {
	//check unique
	bp := &blockProducer{RaftID: id, Url: url, PeerID: peerID}

	for prevID, prevBP := range cc.Member {
		if prevID == id {
			return ErrDupBP
		}

		if !prevBP.isDifferent(bp) {
			return ErrDupBP
		}
	}

	// check if mapping between raft id and PeerID is valid
	if cc.ID == id && peerID != p2p.NodeID() {
		return ErrInvalidRaftPeerID
	}

	cc.Member[id] = bp
	cc.Index[bp.PeerID] = id
	cc.BPUrls[id-1] = bp.Url

	return nil
}

func MaxUint64(x, y uint64) uint64 {
	if x < y {
		return y
	}
	return x
}

// hasSynced get result of GetPeers request from P2P service and check if chain of this node is synchronized with majority of members
func (cc *Cluster) hasSynced() (bool, error) {
	var peers map[peer.ID]*message.PeerInfo
	var err error
	var peerBestNo uint64 = 0

	if cc.Size == 1 {
		return true, nil
	}

	// request GetPeers to p2p
	getBPPeers := func() (map[peer.ID]*message.PeerInfo, error) {
		peers := make(map[peer.ID]*message.PeerInfo)

		result, err := cc.RequestFuture(message.P2PSvc, &message.GetPeers{}, time.Second, "raft cluster sync test").Result()
		if err != nil {
			return nil, err
		}

		msg := result.(*message.GetPeersRsp)

		for _, peerElem := range msg.Peers {
			peerID := peer.ID(peerElem.Addr.PeerID)
			state := peerElem.State

			if peerElem.Self {
				continue
			}

			if state.Get() != types.RUNNING {
				logger.Debug().Str("peer", p2putil.ShortForm(peerID)).Msg("peer is not running")
				continue

			}

			// check if peer is not bp
			if _, ok := cc.Index[peerID]; !ok {
				continue
			}

			peers[peerID] = peerElem

			peerBestNo = MaxUint64(peerElem.LastBlockNumber, peerBestNo)
		}

		return peers, nil
	}

	if peers, err = getBPPeers(); err != nil {
		return false, err
	}

	if uint16(len(peers)) < (cc.Quorum() - 1) {
		logger.Debug().Msg("a majority of peers are not connected")
		return false, nil
	}

	var best *types.Block
	if best, err = cc.cdb.GetBestBlock(); err != nil {
		return false, err
	}

	if best.BlockNo()+DefaultMarginChainDiff < peerBestNo {
		logger.Debug().Uint64("best", best.BlockNo()).Uint64("peerbest", peerBestNo).Msg("chain was not synced with majority of peers")
		return false, nil
	}

	logger.Debug().Uint64("best", best.BlockNo()).Uint64("peerbest", peerBestNo).Int("margin", DefaultMarginChainDiff).Msg("chain has been synced with majority of peers")

	return true, nil
}

func (cc *Cluster) toString() string {
	var buf string

	buf = fmt.Sprintf("raft cluster configure: total=%d, RaftID=%d, bps=[", cc.Size, cc.ID)
	for _, bp := range cc.Member {
		bpbuf := fmt.Sprintf("{ id:%d, Url:%s, PeerID:%s }", bp.RaftID, bp.Url, bp.PeerID)
		buf += bpbuf
	}
	fmt.Sprintf("]")

	return buf
}

func (cl *Cluster) getRaftInfo(withStatus bool) *RaftInfo {
	var leader uint64
	if cl.rs != nil {
		leader = cl.rs.GetLeader()
	}

	rinfo := &RaftInfo{Leader: leader, Total: cl.Size, RaftId: cl.ID}

	if withStatus && cl.rs != nil {
		b, err := cl.rs.Status().MarshalJSON()
		if err != nil {
			logger.Error().Err(err).Msg("failed to marshal raft consensus")
		} else {
			m := json.RawMessage(b)
			rinfo.Status = &m
		}
	}
	return rinfo
}

func (cl *Cluster) toConsensusInfo() *types.ConsensusInfo {
	emptyCons := types.ConsensusInfo{
		Type: GetName(),
	}

	type PeerInfo struct {
		RaftID uint64
		PeerID string
	}

	b, err := json.Marshal(cl.getRaftInfo(true))
	if err != nil {
		logger.Error().Err(err).Msg("failed to marshal raft consensus")
		return &emptyCons
	}

	cons := emptyCons
	cons.Info = string(b)

	var i int = 0
	bps := make([]string, cl.Size)
	for id, m := range cl.Member {
		bp := &PeerInfo{RaftID: m.RaftID, PeerID: m.PeerID.Pretty()}
		b, err = json.Marshal(bp)
		if err != nil {
			logger.Error().Err(err).Uint64("raftid", id).Msg("failed to marshal raft consensus bp")
			return &emptyCons
		}
		bps[i] = string(b)

		i++
	}
	cons.Bps = bps

	return &cons
}
