package cosipbft

import (
	"context"

	"github.com/rs/zerolog"
	"go.dedis.ch/dela/core"
	"go.dedis.ch/dela/core/ordering"
	"go.dedis.ch/dela/core/ordering/cosipbft/authority"
	"go.dedis.ch/dela/core/ordering/cosipbft/blockstore"
	"go.dedis.ch/dela/core/ordering/cosipbft/blocksync"
	"go.dedis.ch/dela/core/ordering/cosipbft/pbft"
	"go.dedis.ch/dela/core/ordering/cosipbft/types"
	"go.dedis.ch/dela/core/store"
	"go.dedis.ch/dela/core/store/hashtree"
	"go.dedis.ch/dela/core/txn/pool"
	"go.dedis.ch/dela/crypto"
	"go.dedis.ch/dela/mino"
	"go.dedis.ch/dela/serde"
	"go.dedis.ch/dela/serde/json"
	"golang.org/x/xerrors"
)

var (
	keyRoster = [32]byte{}
)

// Processor processes the messages to run a collective signing PBFT consensus.
type processor struct {
	mino.UnsupportedHandler
	types.MessageFactory

	logger      zerolog.Logger
	pbftsm      pbft.StateMachine
	sync        blocksync.Synchronizer
	tree        blockstore.TreeCache
	pool        pool.Pool
	watcher     core.Observable
	rosterFac   authority.Factory
	hashFactory crypto.HashFactory

	context serde.Context
	genesis blockstore.GenesisStore
	blocks  blockstore.BlockStore

	started chan struct{}
}

func newProcessor() *processor {
	return &processor{
		watcher: core.NewWatcher(),
		context: json.NewContext(),
		started: make(chan struct{}),
	}
}

// Invoke implements cosi.Reactor. It processes the messages from the collective
// signature module. The messages are either from the the prepare or the commit
// phase.
func (h *processor) Invoke(from mino.Address, msg serde.Message) ([]byte, error) {
	switch in := msg.(type) {
	case types.BlockMessage:
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		blocks := h.blocks.Watch(ctx)

		// In case the node is falling behind the chain, it gives it a chance to
		// catch up before moving forward.
		if h.sync.GetLatest() > h.blocks.Len() {
			for link := range blocks {
				if link.GetBlock().GetIndex() >= h.sync.GetLatest() {
					cancel()
				}
			}
		}

		digest, err := h.pbftsm.Prepare(in.GetBlock())
		if err != nil {
			return nil, xerrors.Errorf("pbft prepare failed: %v", err)
		}

		return digest[:], nil
	case types.CommitMessage:
		err := h.pbftsm.Commit(in.GetID(), in.GetSignature())
		if err != nil {
			h.logger.Info().Msg("commit failed")
			return nil, xerrors.Errorf("pbft commit failed: %v", err)
		}

		buffer, err := in.GetSignature().MarshalBinary()
		if err != nil {
			return nil, xerrors.Errorf("couldn't marshal signature: %v", err)
		}

		return buffer, nil
	default:
		return nil, xerrors.Errorf("unsupported message of type '%T'", msg)
	}
}

// Process implements mino.Handler. It processes the messages from the RPC.
func (h *processor) Process(req mino.Request) (serde.Message, error) {
	switch msg := req.Message.(type) {
	case types.GenesisMessage:
		if h.genesis.Exists() {
			return nil, nil
		}

		root := msg.GetGenesis().GetRoot()

		return nil, h.storeGenesis(msg.GetGenesis().GetRoster(), &root)
	case types.DoneMessage:
		err := h.pbftsm.Finalize(msg.GetID(), msg.GetSignature())
		if err != nil {
			return nil, xerrors.Errorf("pbftsm finalized failed: %v", err)
		}
	case types.ViewMessage:
		view := pbft.View{
			From:   req.Address,
			ID:     msg.GetID(),
			Leader: msg.GetLeader(),
		}

		h.pbftsm.Accept(view)
	default:
		return nil, xerrors.Errorf("unsupported message of type '%T'", req.Message)
	}

	return nil, nil
}

func (h *processor) getCurrentRoster() (authority.Authority, error) {
	return h.readRoster(h.tree.Get())
}

func (h *processor) readRoster(tree hashtree.Tree) (authority.Authority, error) {
	data, err := tree.Get(keyRoster[:])
	if err != nil {
		return nil, xerrors.Errorf("read from tree: %v", err)
	}

	roster, err := h.rosterFac.AuthorityOf(h.context, data)
	if err != nil {
		return nil, xerrors.Errorf("decode failed: %v", err)
	}

	return roster, nil
}

func (h *processor) storeGenesis(roster authority.Authority, match *types.Digest) error {
	value, err := roster.Serialize(h.context)
	if err != nil {
		return xerrors.Errorf("failed to serialize roster: %v", err)
	}

	stageTree, err := h.tree.Get().Stage(func(snap store.Snapshot) error {
		return snap.Set(keyRoster[:], value)
	})
	if err != nil {
		return xerrors.Errorf("tree stage failed: %v", err)
	}

	root := types.Digest{}
	copy(root[:], stageTree.GetRoot())

	if match != nil && *match != root {
		return xerrors.Errorf("mismatch tree root '%v' != '%v'", match, root)
	}

	genesis, err := types.NewGenesis(roster, types.WithGenesisRoot(root))
	if err != nil {
		return xerrors.Errorf("creating genesis: %v", err)
	}

	err = stageTree.Commit()
	if err != nil {
		return xerrors.Errorf("tree commit failed: %v", err)
	}

	h.tree.Set(stageTree)

	err = h.genesis.Set(genesis)
	if err != nil {
		return xerrors.Errorf("set genesis failed: %v", err)
	}

	h.watcher.Notify(ordering.Event{Index: 0})

	close(h.started)

	return nil
}