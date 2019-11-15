package contract

// helper functions
import (
	"errors"
	"fmt"
	"io/ioutil"
	"math"
	"math/big"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/aergoio/aergo-lib/db"
	"github.com/aergoio/aergo/cmd/aergoluac/util"
	"github.com/aergoio/aergo/config"
	"github.com/aergoio/aergo/contract/system"
	"github.com/aergoio/aergo/fee"
	"github.com/aergoio/aergo/internal/enc"
	"github.com/aergoio/aergo/state"
	"github.com/aergoio/aergo/types"
	"github.com/minio/sha256-simd"
)

type DummyChain struct {
	sdb           *state.ChainStateDB
	bestBlock     *types.Block
	cBlock        *types.Block
	bestBlockNo   types.BlockNo
	bestBlockId   types.BlockID
	blockIds      []types.BlockID
	blocks        []*types.Block
	testReceiptDB db.DB
	tmpDir        string
	timeout       int
	clearLState   func()
	gasPrice      *big.Int
}

var addressRegexp *regexp.Regexp
var traceState bool

func init() {
	addressRegexp, _ = regexp.Compile("^[a-zA-Z0-9]+$")
	//	traceState = true
}

func LoadDummyChain(opts ...func(d *DummyChain)) (*DummyChain, error) {
	dataPath, err := ioutil.TempDir("", "data")
	if err != nil {
		return nil, err
	}
	bc := &DummyChain{
		sdb:      state.NewChainStateDB(),
		tmpDir:   dataPath,
		gasPrice: new(big.Int).SetUint64(1),
	}
	defer func() {
		if err != nil {
			bc.Release()
		}
	}()

	err = bc.sdb.Init(string(db.BadgerImpl), dataPath, nil, false)
	if err != nil {
		return nil, err
	}
	genesis := types.GetTestGenesis()
	bc.sdb.SetGenesis(genesis, nil)
	bc.bestBlock = genesis.Block()
	bc.bestBlockNo = genesis.Block().BlockNo()
	bc.bestBlockId = genesis.Block().BlockID()
	bc.blockIds = append(bc.blockIds, bc.bestBlockId)
	bc.blocks = append(bc.blocks, genesis.Block())
	bc.testReceiptDB = db.NewDB(db.BadgerImpl, path.Join(dataPath, "receiptDB"))
	loadTestDatabase(dataPath) // sql database
	SetStateSQLMaxDBSize(1024)
	StartLStateFactory()
	HardforkConfig = config.AllEnabledHardforkConfig

	// To pass the governance tests.
	types.InitGovernance("dpos", true)
	system.InitGovernance("dpos")

	// To pass dao parameters test
	scs, err := bc.sdb.GetStateDB().OpenContractStateAccount(types.ToAccountID([]byte("aergo.system")))
	system.InitSystemParams(scs, 3)

	fee.EnableZeroFee()

	for _, opt := range opts {
		opt(bc)
	}
	return bc, nil
}

func (bc *DummyChain) Release() {
	bc.testReceiptDB.Close()
	if bc.clearLState != nil {
		bc.clearLState()
	}
	_ = os.RemoveAll(bc.tmpDir)
}

func (bc *DummyChain) BestBlockNo() uint64 {
	return bc.bestBlockNo
}

func (bc *DummyChain) newBState() *state.BlockState {
	bc.cBlock = &types.Block{
		Header: &types.BlockHeader{
			PrevBlockHash: bc.bestBlockId[:],
			BlockNo:       bc.bestBlockNo + 1,
			Timestamp:     time.Now().UnixNano(),
			ChainID:       types.MakeChainId(bc.bestBlock.GetHeader().ChainID, HardforkConfig.Version(bc.bestBlockNo+1)),
		},
	}
	return state.NewBlockState(
		bc.sdb.OpenNewStateDB(bc.sdb.GetRoot()),
		state.SetPrevBlockHash(bc.cBlock.GetHeader().PrevBlockHash),
		state.SetGasPrice(bc.gasPrice),
	)
}

func (bc *DummyChain) BeginReceiptTx() db.Transaction {
	return bc.testReceiptDB.NewTx()
}

func (bc *DummyChain) GetABI(contract string) (*types.ABI, error) {
	cState, err := bc.sdb.GetStateDB().OpenContractStateAccount(types.ToAccountID(strHash(contract)))
	if err != nil {
		return nil, err
	}
	return GetABI(cState)
}

func (bc *DummyChain) GetEvents(txhash []byte) []*types.Event {
	receipt := bc.GetReceipt(txhash)
	if receipt != nil {
		return receipt.Events
	}

	return nil
}

func (bc *DummyChain) GetReceipt(txHash []byte) *types.Receipt {
	r := new(types.Receipt)
	r.UnmarshalBinaryTest(bc.testReceiptDB.Get(txHash))
	return r
}

func (bc *DummyChain) GetAccountState(name string) (*types.State, error) {
	return bc.sdb.GetStateDB().GetAccountState(types.ToAccountID(strHash(name)))
}

func (bc *DummyChain) GetStaking(name string) (*types.Staking, error) {
	scs, err := bc.sdb.GetStateDB().OpenContractStateAccount(types.ToAccountID([]byte(types.AergoSystem)))
	if err != nil {
		return nil, err
	}
	return system.GetStaking(scs, strHash(name))
}

func (bc *DummyChain) GetBlockByNo(blockNo types.BlockNo) (*types.Block, error) {
	return bc.blocks[blockNo], nil
}

func (bc *DummyChain) GetBestBlock() (*types.Block, error) {
	return bc.bestBlock, nil
}

type luaTx interface {
	run(bs *state.BlockState, bc *DummyChain, bi *types.BlockHeaderInfo, receiptTx db.Transaction) error
}

type luaTxAccount struct {
	name    []byte
	balance *big.Int
}

var _ luaTx = (*luaTxAccount)(nil)

func NewLuaTxAccount(name string, balance uint64) *luaTxAccount {
	return NewLuaTxAccountBig(name, new(big.Int).SetUint64(balance))
}

func NewLuaTxAccountBig(name string, balance *big.Int) *luaTxAccount {
	return &luaTxAccount{
		name:    strHash(name),
		balance: balance,
	}
}

func (l *luaTxAccount) run(bs *state.BlockState, bc *DummyChain, bi *types.BlockHeaderInfo, receiptTx db.Transaction) error {
	id := types.ToAccountID(l.name)
	accountState, err := bs.GetAccountState(id)
	if err != nil {
		return err
	}
	updatedAccountState := types.State(*accountState)
	updatedAccountState.Balance = l.balance.Bytes()
	bs.PutState(id, &updatedAccountState)
	return nil
}

type luaTxSend struct {
	sender   []byte
	receiver []byte
	balance  *big.Int
}

var _ luaTx = (*luaTxSend)(nil)

func NewLuaTxSendBig(sender, receiver string, balance *big.Int) *luaTxSend {
	return &luaTxSend{
		sender:   strHash(sender),
		receiver: strHash(receiver),
		balance:  balance,
	}
}

func (l *luaTxSend) run(bs *state.BlockState, bc *DummyChain, bi *types.BlockHeaderInfo, receiptTx db.Transaction) error {
	senderID := types.ToAccountID(l.sender)
	receiverID := types.ToAccountID(l.receiver)

	if senderID == receiverID {
		return fmt.Errorf("sender and receiever cannot be same")
	}

	senderState, err := bs.GetAccountState(senderID)
	if err != nil {
		return err
	} else if senderState.GetBalanceBigInt().Cmp(l.balance) < 0 {
		return fmt.Errorf("insufficient balance to sender")
	}
	receiverState, err := bs.GetAccountState(receiverID)
	if err != nil {
		return err
	}

	updatedSenderState := types.State(*senderState)
	updatedSenderState.Balance = new(big.Int).Sub(updatedSenderState.GetBalanceBigInt(), l.balance).Bytes()
	bs.PutState(senderID, &updatedSenderState)

	updatedReceiverState := types.State(*receiverState)
	updatedReceiverState.Balance = new(big.Int).Add(updatedReceiverState.GetBalanceBigInt(), l.balance).Bytes()
	bs.PutState(receiverID, &updatedReceiverState)

	return nil
}

type luaTxCommon struct {
	sender      []byte
	contract    []byte
	amount      *big.Int
	code        []byte
	id          uint64
	feeDelegate bool
}

func (l *luaTxCommon) Hash() []byte {
	h := sha256.New()
	h.Write([]byte(strconv.FormatUint(l.id, 10)))
	b := h.Sum(nil)
	return b
}

type luaTxDef struct {
	luaTxCommon
	cErr error
}

var _ luaTx = (*luaTxDef)(nil)

func NewLuaTxDef(sender, contract string, amount uint64, code string) *luaTxDef {
	return NewLuaTxDefBig(sender, contract, new(big.Int).SetUint64(amount), code)
}

func NewLuaTxDefBig(sender, contract string, amount *big.Int, code string) *luaTxDef {
	byteCode, err := compile(code, nil)
	if err != nil {
		return &luaTxDef{cErr: err}
	}
	return &luaTxDef{
		luaTxCommon: luaTxCommon{
			sender:   strHash(sender),
			contract: strHash(contract),
			code:     util.NewLuaCodePayload(byteCode, nil),
			amount:   amount,
			id:       newTxId(),
		},
		cErr: nil,
	}
}

func strHash(d string) []byte {
	// using real address
	if len(d) == types.EncodedAddressLength && addressRegexp.MatchString(d) {
		return types.ToAddress(d)
	} else {
		// using alias
		h := sha256.New()
		h.Write([]byte(d))
		b := h.Sum(nil)
		b = append([]byte{0x0C}, b...)
		return b
	}
}

var luaTxId uint64 = 0

func newTxId() uint64 {
	luaTxId++
	return luaTxId
}

func (l *luaTxDef) Constructor(args string) *luaTxDef {
	if len(args) == 0 || strings.Compare(args, "[]") == 0 || l.cErr != nil {
		return l
	}
	l.code = util.NewLuaCodePayload(util.LuaCodePayload(l.code).Code(), []byte(args))
	return l
}

func contractFrame(l *luaTxCommon, bs *state.BlockState,
	run func(s, c *state.V, id types.AccountID, cs *state.ContractState) (*big.Int, error)) error {

	creatorId := types.ToAccountID(l.sender)
	creatorState, err := bs.GetAccountStateV(l.sender)
	if err != nil {
		return err
	}

	contractId := types.ToAccountID(l.contract)
	contractState, err := bs.GetAccountStateV(l.contract)
	if err != nil {
		return err
	}

	eContractState, err := bs.OpenContractState(contractId, contractState.State())
	if err != nil {
		return err
	}
	usedFee := txFee(len(l.code), new(big.Int).SetUint64(1), 2)

	if l.feeDelegate {
		balance := contractState.Balance()

		if usedFee.Cmp(balance) > 0 {
			return types.ErrInsufficientBalance
		}
		err = CheckFeeDelegation(l.contract, bs, nil, eContractState, l.code,
			l.Hash(), l.sender, l.amount.Bytes())
		if err != nil {
			if err != types.ErrNotAllowedFeeDelegation {
				ctrLgr.Debug().Err(err).Str("txhash", enc.ToString(l.Hash())).Msg("checkFeeDelegation Error")
				return err
			}
			return types.ErrNotAllowedFeeDelegation
		}
	}
	creatorState.SubBalance(l.amount)
	contractState.AddBalance(l.amount)
	cFee, err := run(creatorState, contractState, contractId, eContractState)
	if err != nil {
		return err
	}
	if cFee != nil {
		usedFee.Add(usedFee, cFee)
	}
	if l.feeDelegate {
		if contractState.Balance().Cmp(usedFee) < 0 {
			return types.ErrInsufficientBalance
		}
		contractState.SubBalance(usedFee)
	} else {
		if creatorState.Balance().Cmp(usedFee) < 0 {
			return types.ErrInsufficientBalance
		}
		creatorState.SubBalance(usedFee)
	}
	bs.PutState(creatorId, creatorState.State())
	bs.PutState(contractId, contractState.State())
	return nil

}

func (l *luaTxDef) run(bs *state.BlockState, bc *DummyChain, bi *types.BlockHeaderInfo, receiptTx db.Transaction) error {
	if l.cErr != nil {
		return l.cErr
	}
	return contractFrame(&l.luaTxCommon, bs,
		func(sender, contract *state.V, contractId types.AccountID, eContractState *state.ContractState) (*big.Int, error) {
			contract.State().SqlRecoveryPoint = 1

			ctx := newVmContext(bs, nil, sender, contract, eContractState, sender.ID(), l.Hash(), bi, "", true,
				false, contract.State().SqlRecoveryPoint, BlockFactory, l.luaTxCommon.amount, math.MaxUint64, false)

			if traceState {
				ctx.traceFile, _ =
					os.OpenFile("test.trace", os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
				defer ctx.traceFile.Close()
			}

			_, _, ctrFee, err := Create(eContractState, l.code, l.contract, ctx)
			if err != nil {
				r := types.NewReceipt(contract.ID(), "ERROR", err.Error())
				r.TxHash = l.Hash()
				r.GasUsed = ctrFee.Uint64()
				b, _ := r.MarshalBinaryTest()
				receiptTx.Set(l.Hash(), b)
				return ctrFee, err
			}
			err = bs.StageContractState(eContractState)
			if err != nil {
				return ctrFee, err
			}
			r := types.NewReceipt(contract.ID(), "CREATED", "")
			r.TxHash = l.Hash()
			r.GasUsed = ctrFee.Uint64()
			b, _ := r.MarshalBinaryTest()
			receiptTx.Set(l.Hash(), b)
			return ctrFee, nil
		},
	)
}

type luaTxCall struct {
	luaTxCommon
	expectedErr string
}

var _ luaTx = (*luaTxCall)(nil)

func NewLuaTxCall(sender, contract string, amount uint64, code string) *luaTxCall {
	return NewLuaTxCallBig(sender, contract, new(big.Int).SetUint64(amount), code)
}

func NewLuaTxCallBig(sender, contract string, amount *big.Int, code string) *luaTxCall {
	return &luaTxCall{
		luaTxCommon: luaTxCommon{
			sender:   strHash(sender),
			contract: strHash(contract),
			amount:   amount,
			code:     []byte(code),
			id:       newTxId(),
		},
	}
}

func NewLuaTxCallFeeDelegate(sender, contract string, amount uint64, code string) *luaTxCall {
	return &luaTxCall{
		luaTxCommon: luaTxCommon{
			sender:      strHash(sender),
			contract:    strHash(contract),
			amount:      new(big.Int).SetUint64(amount),
			code:        []byte(code),
			id:          newTxId(),
			feeDelegate: true,
		},
	}
}

func (l *luaTxCall) Fail(expectedErr string) *luaTxCall {
	l.expectedErr = expectedErr
	return l
}

func (l *luaTxCall) run(bs *state.BlockState, bc *DummyChain, bi *types.BlockHeaderInfo, receiptTx db.Transaction) error {
	err := contractFrame(&l.luaTxCommon, bs,
		func(sender, contract *state.V, contractId types.AccountID, eContractState *state.ContractState) (*big.Int, error) {
			ctx := newVmContext(bs, bc, sender, contract, eContractState, sender.ID(), l.Hash(), bi, "", true,
				false, contract.State().SqlRecoveryPoint, BlockFactory, l.luaTxCommon.amount, math.MaxUint64, l.feeDelegate)
			if traceState {
				ctx.traceFile, _ =
					os.OpenFile("test.trace", os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
				defer ctx.traceFile.Close()
			}
			rv, evs, ctrFee, err := Call(eContractState, l.code, l.contract, ctx)
			if err != nil {
				r := types.NewReceipt(l.contract, "ERROR", err.Error())
				r.TxHash = l.Hash()
				r.GasUsed = ctrFee.Uint64()
				b, _ := r.MarshalBinaryTest()
				receiptTx.Set(l.Hash(), b)
				return ctrFee, err
			}
			_ = bs.StageContractState(eContractState)
			r := types.NewReceipt(l.contract, "SUCCESS", rv)
			r.Events = evs
			r.TxHash = l.Hash()
			r.GasUsed = ctrFee.Uint64()
			blockHash := make([]byte, 32)
			for _, ev := range evs {
				ev.TxHash = r.TxHash
				ev.BlockHash = blockHash
			}
			b, _ := r.MarshalBinaryTest()
			receiptTx.Set(l.Hash(), b)
			return ctrFee, nil
		},
	)
	if l.expectedErr != "" {
		if err == nil {
			return fmt.Errorf("no error, expected: %s", l.expectedErr)
		}
		if !strings.Contains(err.Error(), l.expectedErr) {
			return err
		}
		return nil
	}
	return err
}

func (bc *DummyChain) ConnectBlock(txs ...luaTx) error {
	blockState := bc.newBState()
	tx := bc.BeginReceiptTx()
	defer tx.Commit()
	defer CloseDatabase()

	timeout := make(chan struct{})
	go func() {
		if bc.timeout != 0 {
			<-time.Tick(time.Duration(bc.timeout) * time.Millisecond)
			timeout <- struct{}{}
		}
	}()
	SetBPTimeout(timeout)
	for _, x := range txs {
		if err := x.run(blockState, bc, types.NewBlockHeaderInfo(bc.cBlock), tx); err != nil {
			return err
		}
	}
	err := SaveRecoveryPoint(blockState)
	if err != nil {
		return err
	}
	err = bc.sdb.Apply(blockState)
	if err != nil {
		return err
	}
	//FIXME newblock must be created after sdb.apply()
	bc.cBlock.SetBlocksRootHash(bc.sdb.GetRoot())
	bc.bestBlockNo = bc.bestBlockNo + 1
	bc.bestBlock = bc.cBlock
	bc.bestBlockId = types.ToBlockID(bc.cBlock.BlockHash())
	bc.blockIds = append(bc.blockIds, bc.bestBlockId)
	bc.blocks = append(bc.blocks, bc.cBlock)

	return nil
}

func (bc *DummyChain) DisConnectBlock() error {
	if len(bc.blockIds) == 1 {
		return errors.New("genesis block")
	}
	bc.bestBlockNo--
	bc.blockIds = bc.blockIds[0 : len(bc.blockIds)-1]
	bc.blocks = bc.blocks[0 : len(bc.blocks)-1]
	bc.bestBlockId = bc.blockIds[len(bc.blockIds)-1]

	bestBlock := bc.blocks[len(bc.blocks)-1]

	var sroot []byte
	if bestBlock != nil {
		sroot = bestBlock.GetHeader().GetBlocksRootHash()
	}
	return bc.sdb.SetRoot(sroot)
}

func (bc *DummyChain) Query(contract, queryInfo, expectedErr string, expectedRvs ...string) error {
	cState, err := bc.sdb.GetStateDB().OpenContractStateAccount(types.ToAccountID(strHash(contract)))
	if err != nil {
		return err
	}
	rv, err := Query(strHash(contract), bc.newBState(), bc, cState, []byte(queryInfo))
	if expectedErr != "" {
		if err == nil {
			return fmt.Errorf("no error, expected: %s", expectedErr)
		}
		if !strings.Contains(err.Error(), expectedErr) {
			return err
		}
		return nil
	}
	if err != nil {
		return err
	}

	for _, ev := range expectedRvs {
		if ev != string(rv) {
			err = fmt.Errorf("expected: %s, but got: %s", ev, string(rv))
		} else {
			return nil
		}
	}
	return err
}

func (bc *DummyChain) QueryOnly(contract, queryInfo string, expectedErr string) (bool, string, error) {
	cState, err := bc.sdb.GetStateDB().OpenContractStateAccount(types.ToAccountID(strHash(contract)))
	if err != nil {
		return false, "", err
	}
	rv, err := Query(strHash(contract), bc.newBState(), nil, cState, []byte(queryInfo))

	if expectedErr != "" {
		if err == nil {
			return false, "", fmt.Errorf("no error, expected: %s", expectedErr)
		}
		if !strings.Contains(err.Error(), expectedErr) {
			return false, "", err
		}
		return true, "", nil
	}

	if err != nil {
		return false, "", err
	}

	return false, string(rv), nil
}

func StrToAddress(name string) string {
	return types.EncodeAddress(strHash(name))
}

func OnPubNet(dc *DummyChain) {
	flushLState := func() {
		for i := 0; i <= lStateMaxSize; i++ {
			s := getLState()
			freeLState(s)
		}
	}
	PubNet = true
	fee.DisableZeroFee()
	flushLState()

	dc.clearLState = func() {
		PubNet = false
		fee.EnableZeroFee()
		flushLState()
	}
}
