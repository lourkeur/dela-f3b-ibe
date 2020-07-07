package json

import (
	"encoding/json"

	"go.dedis.ch/dela/blockchain"
	"go.dedis.ch/dela/blockchain/skipchain/types"
	"go.dedis.ch/dela/consensus"
	"go.dedis.ch/dela/crypto"
	"go.dedis.ch/dela/serde"
	"golang.org/x/xerrors"
)

func init() {
	types.RegisterBlockFormat(serde.FormatJSON, blockFormat{})
	types.RegisterVerifiableBlockFormats(serde.FormatJSON, newVerifiableFormat())
	types.RegisterBlueprintFormat(serde.FormatJSON, blueprintFormat{})
	types.RegisterRequestFormat(serde.FormatJSON, newRequestFormat())
}

// Blueprint is a JSON message to send a proposal.
type Blueprint struct {
	Index    uint64
	Previous []byte
	Payload  []byte
}

// SkipBlock is the JSON message for a block.
type SkipBlock struct {
	Index     uint64
	GenesisID []byte
	Backlink  []byte
	Payload   json.RawMessage
}

// VerifiableBlock is the JSON message for a verifiable block.
type VerifiableBlock struct {
	Block json.RawMessage
	Chain json.RawMessage
}

// PropagateGenesis the the JSON message to share a genesis block.
type PropagateGenesis struct {
	Genesis json.RawMessage
}

// BlockRequest is the JSON message to request a chain of blocks.
type BlockRequest struct {
	From uint64
	To   uint64
}

// BlockResponse is the response of a block request.
type BlockResponse struct {
	Block json.RawMessage
}

// Request is the container of the request messages.
type Request struct {
	Propagate *PropagateGenesis `json:",omitempty"`
	Request   *BlockRequest     `json:",omitempty"`
	Response  *BlockResponse    `json:",omitempty"`
}

// BlockFormat is the engine to encode and decode block messages in JSON format.
//
// - implements serde.FormatEngine
type blockFormat struct {
	hashFactory crypto.HashFactory
}

// Encode implements serde.FormatEngine. It returns the serialized block if
// appropriate, otherwise an error.
func (f blockFormat) Encode(ctx serde.Context, msg serde.Message) ([]byte, error) {
	block, ok := msg.(types.SkipBlock)
	if !ok {
		return nil, xerrors.Errorf("unsupported message of type '%T'", msg)
	}

	payload, err := block.Payload.Serialize(ctx)
	if err != nil {
		return nil, xerrors.Errorf("couldn't serialize payload: %v", err)
	}

	m := SkipBlock{
		Index:     block.GetIndex(),
		GenesisID: block.GenesisID.Bytes(),
		Backlink:  block.BackLink.Bytes(),
		Payload:   payload,
	}

	data, err := ctx.Marshal(m)
	if err != nil {
		return nil, xerrors.Errorf("couldn't marshal: %v", err)
	}

	return data, nil
}

// Decode implements serde.FormatEngine. It populates the block of the JSON data
// if appropriate, otherwise it returns an error.
func (f blockFormat) Decode(ctx serde.Context, data []byte) (serde.Message, error) {
	m := SkipBlock{}
	err := ctx.Unmarshal(data, &m)
	if err != nil {
		return nil, xerrors.Errorf("couldn't deserialize message: %v", err)
	}

	factory := ctx.GetFactory(types.PayloadKeyFac{})
	if factory == nil {
		return nil, xerrors.New("payload factory is missing")
	}

	msg, err := factory.Deserialize(ctx, m.Payload)
	if err != nil {
		return nil, xerrors.Errorf("couldn't deserialize payload: %v", err)
	}

	payload, ok := msg.(blockchain.Payload)
	if !ok {
		return nil, xerrors.Errorf("invalid payload of type '%T'", msg)
	}

	opts := []types.SkipBlockOption{
		types.WithIndex(m.Index),
		types.WithGenesisID(m.GenesisID),
		types.WithBackLink(m.Backlink),
	}

	if f.hashFactory != nil {
		// Keep the skipblock default factory unless it is effectively set.
		opts = append(opts, types.WithHashFactory(f.hashFactory))
	}

	block, err := types.NewSkipBlock(payload, opts...)

	if err != nil {
		return nil, xerrors.Errorf("couldn't create block: %v", err)
	}

	return block, nil
}

// VerifiableFormat is the engine to encode and decode verifiable block messages
// in JSON format.
//
// - implements serde.FormatEngine
type verifiableFormat struct {
	blockFormat serde.FormatEngine
}

func newVerifiableFormat() verifiableFormat {
	return verifiableFormat{
		blockFormat: blockFormat{},
	}
}

// Encode implements serde.FormatEngine. It returns the serialized block if
// appropriate, otherwise an error.
func (f verifiableFormat) Encode(ctx serde.Context, msg serde.Message) ([]byte, error) {
	vb, ok := msg.(types.VerifiableBlock)
	if !ok {
		return nil, xerrors.Errorf("unsupported message of type '%T'", msg)
	}

	block, err := f.blockFormat.Encode(ctx, vb.SkipBlock)
	if err != nil {
		return nil, xerrors.Errorf("couldn't serialize block: %v", err)
	}

	chain, err := vb.Chain.Serialize(ctx)
	if err != nil {
		return nil, xerrors.Errorf("couldn't serialize chain: %v", err)
	}

	m := VerifiableBlock{
		Block: block,
		Chain: chain,
	}

	data, err := ctx.Marshal(m)
	if err != nil {
		return nil, xerrors.Errorf("couldn't marshal: %v", err)
	}

	return data, nil
}

// Decode implements serde.FormatEngine. It populates the block for the JSON
// data if appropriate, otherwise it returns an error.
func (f verifiableFormat) Decode(ctx serde.Context, data []byte) (serde.Message, error) {
	m := VerifiableBlock{}
	err := ctx.Unmarshal(data, &m)
	if err != nil {
		return nil, xerrors.Errorf("couldn't deserialize message: %v", err)
	}

	chain, err := decodeChain(ctx, m.Chain)
	if err != nil {
		return nil, err
	}

	block, err := f.blockFormat.Decode(ctx, m.Block)
	if err != nil {
		return types.SkipBlock{}, xerrors.Errorf("couldn't deserialize block: %v", err)
	}

	vb := types.VerifiableBlock{
		SkipBlock: block.(types.SkipBlock),
		Chain:     chain,
	}

	return vb, nil
}

func decodeChain(ctx serde.Context, data []byte) (consensus.Chain, error) {
	factory := ctx.GetFactory(types.ChainKeyFac{})

	fac, ok := factory.(consensus.ChainFactory)
	if !ok {
		return nil, xerrors.Errorf("invalid chain factory of type '%T'", factory)
	}

	chain, err := fac.ChainOf(ctx, data)
	if err != nil {
		return nil, xerrors.Errorf("couldn't deserialize chain: %v", err)
	}

	return chain, nil
}

// BlueprintFormat is the engine to encode and decode blueprint messages in JSON
// format.
//
// - implements serde.FormatEngine
type blueprintFormat struct{}

// Encode implements serde.FormatEngine. It returns the serialized blueprint if
// appropriate, otherwise an error.
func (f blueprintFormat) Encode(ctx serde.Context, msg serde.Message) ([]byte, error) {
	bp, ok := msg.(types.Blueprint)
	if !ok {
		return nil, xerrors.Errorf("unsupported message of type '%T'", msg)
	}

	payload, err := bp.GetData().Serialize(ctx)
	if err != nil {
		return nil, xerrors.Errorf("couldn't serialize payload: %v", err)
	}

	m := Blueprint{
		Index:    bp.GetIndex(),
		Previous: bp.GetPrevious(),
		Payload:  payload,
	}

	data, err := ctx.Marshal(m)
	if err != nil {
		return nil, xerrors.Errorf("couldn't marshal: %v", err)
	}

	return data, nil
}

// Decode implements serde.FormatEngine. It populates the blueprint for the JSON
// data if appropriate, otherwise it returns an error.
func (f blueprintFormat) Decode(ctx serde.Context, data []byte) (serde.Message, error) {
	m := Blueprint{}
	err := ctx.Unmarshal(data, &m)
	if err != nil {
		return nil, xerrors.Errorf("couldn't deserialize blueprint: %v", err)
	}

	factory := ctx.GetFactory(types.DataKeyFac{})
	if factory == nil {
		return nil, xerrors.New("missing data factory")
	}

	payload, err := factory.Deserialize(ctx, m.Payload)
	if err != nil {
		return nil, xerrors.Errorf("couldn't deserialize payload: %v", err)
	}

	b := types.NewBlueprint(m.Index, m.Previous, payload)

	return b, nil
}

// RequestFormat is the engine to encode and decode request messages in JSON
// format.
//
// - implements serde.FormatEngine
type requestFormat struct {
	blockFormat serde.FormatEngine
}

func newRequestFormat() requestFormat {
	return requestFormat{
		blockFormat: blockFormat{},
	}
}

// Encode implements serde.FormatEngine. It returns the serialized request if
// appropriate, otherwise an error.
func (f requestFormat) Encode(ctx serde.Context, msg serde.Message) ([]byte, error) {
	var req Request

	switch in := msg.(type) {
	case types.PropagateGenesis:
		block, err := f.blockFormat.Encode(ctx, in.GetGenesis())
		if err != nil {
			return nil, xerrors.Errorf("couldn't serialize genesis: %v", err)
		}

		m := PropagateGenesis{
			Genesis: block,
		}

		req = Request{Propagate: &m}
	case types.BlockRequest:
		m := BlockRequest{
			From: in.GetFrom(),
			To:   in.GetTo(),
		}

		req = Request{Request: &m}
	case types.BlockResponse:
		block, err := f.blockFormat.Encode(ctx, in.GetBlock())
		if err != nil {
			return nil, xerrors.Errorf("couldn't serialize block: %v", err)
		}

		m := BlockResponse{
			Block: block,
		}

		req = Request{Response: &m}
	default:
		return nil, xerrors.Errorf("unsupported message of type '%T'", msg)
	}

	data, err := ctx.Marshal(req)
	if err != nil {
		return nil, xerrors.Errorf("couldn't marshal: %v", err)
	}

	return data, nil
}

// Decode implements serde.FormatEngine. It populates the request for the JSON
// data if appropriate, otherwise it returns an error.
func (f requestFormat) Decode(ctx serde.Context, data []byte) (serde.Message, error) {
	m := Request{}
	err := ctx.Unmarshal(data, &m)
	if err != nil {
		return nil, xerrors.Errorf("couldn't deserialize message: %v", err)
	}

	if m.Propagate != nil {
		genesis, err := f.blockFormat.Decode(ctx, m.Propagate.Genesis)
		if err != nil {
			return nil, xerrors.Errorf("couldn't deserialize genesis: %v", err)
		}

		p := types.NewPropagateGenesis(genesis.(types.SkipBlock))

		return p, nil
	}

	if m.Request != nil {
		req := types.NewBlockRequest(m.Request.From, m.Request.To)

		return req, nil
	}

	if m.Response != nil {
		block, err := f.blockFormat.Decode(ctx, m.Response.Block)
		if err != nil {
			return nil, xerrors.Errorf("couldn't deserialize block: %v", err)
		}

		resp := types.NewBlockResponse(block.(types.SkipBlock))

		return resp, nil
	}

	return nil, xerrors.New("message is empty")
}