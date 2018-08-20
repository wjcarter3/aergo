/**
 *  @file
 *  @copyright defined in aergo/LICENSE.txt
 */

package blockchain

import (
	"errors"
	"sort"

	"github.com/aergoio/aergo-lib/db"
	"github.com/aergoio/aergo/state"
	"github.com/aergoio/aergo/types"
	peer "github.com/libp2p/go-libp2p-peer"
)

const minimum = 100

func (cs *ChainService) processVoteTx(dbtx *db.Transaction, bs *state.BlockState, txBody *types.TxBody) error {
	if txBody.Amount < minimum {
		return errors.New("too small amount to vote")
	}
	senderKey := types.ToAccountID(txBody.Account)
	senderState, err := cs.sdb.GetAccountClone(bs, senderKey)
	if err != nil {
		return err
	}
	senderChange := types.Clone(*senderState).(types.State)

	voter := types.EncodeB64(txBody.Account)
	c, err := peer.IDFromBytes(txBody.Recipient)
	if err != nil {
		return err
	}
	to := c.Pretty()
	if txBody.GetPrice() == 1 { //stake and vote
		if senderChange.Balance < txBody.Amount {
			return errors.New("not enough balance")
		} else {
			senderChange.Balance = senderState.Balance - txBody.Amount
		}
		cs.putVote(voter, to, int64(txBody.Amount))
		senderChange.Nonce = txBody.Nonce
		bs.PutAccount(senderKey, senderState, &senderChange)

	} else { //unstake and refund
		//TODO: check valid candidate, voter, amount from state db
		if cs.getVote(voter, to) < txBody.Amount {
			return errors.New("not enough staking balance")
		}
		senderChange.Balance = senderState.Balance + txBody.Amount
		bs.PutAccount(senderKey, senderState, &senderChange)
		//TODO: update candidate, voter, amount to state db
		cs.putVote(voter, to, -int64(txBody.Amount))
	}
	return nil
}

func (cs *ChainService) putVote(voter string, to string, amount int64) {
	//TODO: update candidate, voter, amount to state db
	entry, ok := cs.voters[voter]
	if !ok {
		entry = make(map[string]uint64)
		cs.voters[voter] = entry
	}
	entry[to] = uint64(int64(entry[to]) + amount)
	cs.votes[to] = uint64(int64(cs.votes[to]) + amount)
}

func (cs *ChainService) getVote(voter string, to string) uint64 {
	return cs.voters[voter][to]
}

func (cs *ChainService) getVotes(n int) types.VoteList {
	var ret types.VoteList
	tmp := make([]*types.Vote, len(cs.votes))
	ret.Votes = tmp

	i := 0
	for k, v := range cs.votes {
		c := types.DecodeB64(k)
		ret.Votes[i] = &types.Vote{Candidate: c, Amount: v}
		i++
	}
	sort.Sort(sort.Reverse(ret))
	if n < len(cs.votes) {
		ret.Votes = ret.Votes[:n]
	}
	return ret
}
