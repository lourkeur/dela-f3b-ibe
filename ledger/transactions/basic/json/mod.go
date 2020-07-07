// Package json defines the JSON messages for the basic transactions.
package json

import (
	"encoding/json"

	"go.dedis.ch/dela/crypto"
	"go.dedis.ch/dela/ledger/transactions/basic"
	"go.dedis.ch/dela/serde"
	"golang.org/x/xerrors"
)

func init() {
	basic.RegisterTxFormat(serde.FormatJSON, txFormat{})
}

// Task is a container of a task with its type and the raw value to deserialize.
type Task struct {
	Type  string
	Value json.RawMessage
}

// Transaction is a combination of a given task and some metadata including the
// nonce to prevent replay attack and the signature to prove the identity of the
// client.
type Transaction struct {
	Nonce     uint64
	Identity  json.RawMessage
	Signature json.RawMessage
	Task      Task
}

// TxFormat is the format engine to encode and decode transactions.
//
// - implements serde.FormatEngine
type txFormat struct {
	hashFactory crypto.HashFactory
}

// Encode implements serde.FormatEngine. It encodes the transaction to its JSON
// representation.
func (f txFormat) Encode(ctx serde.Context, msg serde.Message) ([]byte, error) {
	var tx basic.ClientTransaction
	switch in := msg.(type) {
	case basic.ClientTransaction:
		tx = in
	case basic.ServerTransaction:
		tx = in.ClientTransaction
	default:
		return nil, xerrors.Errorf("unsupported tx type '%T'", msg)
	}

	identity, err := tx.GetIdentity().Serialize(ctx)
	if err != nil {
		return nil, xerrors.Errorf("couldn't serialize identity: %v", err)
	}

	signature, err := tx.GetSignature().Serialize(ctx)
	if err != nil {
		return nil, xerrors.Errorf("couldn't serialize signature: %v", err)
	}

	task, err := tx.GetTask().Serialize(ctx)
	if err != nil {
		return nil, xerrors.Errorf("couldn't serialize task: %v", err)
	}

	m := Transaction{
		Nonce:     tx.GetNonce(),
		Identity:  identity,
		Signature: signature,
		Task: Task{
			Type:  basic.KeyOf(tx.GetTask()),
			Value: task,
		},
	}

	data, err := ctx.Marshal(m)
	if err != nil {
		return nil, xerrors.Errorf("couldn't marshal: %v", err)
	}

	return data, nil
}

// Decode implements serde.FormatEngine. It decodes the transaction from its
// JSON representation.
func (f txFormat) Decode(ctx serde.Context, data []byte) (serde.Message, error) {
	m := Transaction{}
	err := ctx.Unmarshal(data, &m)
	if err != nil {
		return nil, xerrors.Errorf("couldn't deserialize transaction: %v", err)
	}

	identity, err := decodeIdentity(ctx, m.Identity)
	if err != nil {
		return nil, xerrors.Errorf("couldn't deserialize identity: %v", err)
	}

	signature, err := decodeSignature(ctx, m.Signature)
	if err != nil {
		return nil, xerrors.Errorf("couldn't deserialize signature: %v", err)
	}

	task, err := decodeTask(ctx, m.Task.Type, m.Task.Value)
	if err != nil {
		return nil, xerrors.Errorf("couldn't deserialize task: %v", err)
	}

	opts := []basic.ServerTransactionOption{
		basic.WithNonce(m.Nonce),
		basic.WithIdentity(identity, signature),
		basic.WithTask(task),
	}

	if f.hashFactory != nil {
		opts = append(opts, basic.WithHashFactory(f.hashFactory))
	}

	tx, err := basic.NewServerTransaction(opts...)
	if err != nil {
		return nil, xerrors.Errorf("couldn't create tx: %v", err)
	}

	return tx, nil
}

func decodeIdentity(ctx serde.Context, data []byte) (crypto.PublicKey, error) {
	factory := ctx.GetFactory(basic.IdentityKey{})

	fac, ok := factory.(crypto.PublicKeyFactory)
	if !ok {
		return nil, xerrors.Errorf("invalid factory of type '%T'", factory)
	}

	pubkey, err := fac.PublicKeyOf(ctx, data)
	if err != nil {
		return nil, err
	}

	return pubkey, nil
}

func decodeSignature(ctx serde.Context, data []byte) (crypto.Signature, error) {
	factory := ctx.GetFactory(basic.SignatureKey{})

	fac, ok := factory.(crypto.SignatureFactory)
	if !ok {
		return nil, xerrors.Errorf("invalid factory of type '%T'", factory)
	}

	sig, err := fac.SignatureOf(ctx, data)
	if err != nil {
		return nil, err
	}

	return sig, nil
}

func decodeTask(ctx serde.Context, key string, data []byte) (basic.ServerTask, error) {
	factory := ctx.GetFactory(basic.TaskKey{})

	fac, ok := factory.(basic.TransactionFactory)
	if !ok {
		return nil, xerrors.Errorf("invalid factory of type '%T'", factory)
	}

	taskFac := fac.Get(key)
	if taskFac == nil {
		return nil, xerrors.Errorf("task factory for '%s' not found", key)
	}

	task, err := taskFac.Deserialize(ctx, data)
	if err != nil {
		return nil, err
	}

	return task.(basic.ServerTask), nil
}