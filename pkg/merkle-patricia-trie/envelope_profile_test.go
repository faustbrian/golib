package mpt_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	mpt "github.com/faustbrian/golib/pkg/merkle-patricia-trie"
)

func TestTypedEnvelopeProfilesBindForkActivationAndPayloads(t *testing.T) {
	t.Parallel()

	profiles := []struct {
		name        string
		profile     mpt.ForkProfile
		maximumType byte
	}{
		{name: "berlin", profile: mpt.BerlinProfile, maximumType: 1},
		{name: "london", profile: mpt.LondonProfile, maximumType: 2},
		{name: "paris", profile: mpt.ParisProfile, maximumType: 2},
		{name: "shanghai", profile: mpt.ShanghaiProfile, maximumType: 2},
		{name: "cancun", profile: mpt.CancunProfile, maximumType: 3},
		{name: "prague", profile: mpt.PragueProfile, maximumType: 4},
		{name: "osaka", profile: mpt.OsakaProfile, maximumType: 4},
	}
	for _, test := range profiles {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			for envelopeType := byte(1); envelopeType <= test.maximumType; envelopeType++ {
				transaction, err := mpt.TypedTransactionValue(
					test.profile, envelopeType, []byte{0xc0}, mpt.DefaultLimits(),
				)
				if err != nil {
					t.Fatalf("TypedTransactionValue(%d) error = %v", envelopeType, err)
				}
				receipt, err := mpt.TypedReceiptValue(
					test.profile, envelopeType, []byte{0xc0}, mpt.DefaultLimits(),
				)
				if err != nil {
					t.Fatalf("TypedReceiptValue(%d) error = %v", envelopeType, err)
				}
				want := []byte{envelopeType, 0xc0}
				if !slices.Equal(transaction.Bytes(), want) ||
					!slices.Equal(receipt.Bytes(), want) {
					t.Fatalf("typed envelope %d bytes mismatch", envelopeType)
				}
			}

			if _, err := mpt.TypedTransactionValue(
				test.profile,
				test.maximumType+1,
				[]byte{0xc0},
				mpt.DefaultLimits(),
			); !errors.Is(err, mpt.ErrInvalidEnvelope) {
				t.Fatalf("next transaction type error = %v", err)
			}
			if _, err := mpt.TypedReceiptValue(
				test.profile,
				test.maximumType+1,
				[]byte{0xc0},
				mpt.DefaultLimits(),
			); !errors.Is(err, mpt.ErrInvalidEnvelope) {
				t.Fatalf("next receipt type error = %v", err)
			}
		})
	}

	if _, err := mpt.TypedTransactionValue(
		mpt.ForkProfile(0), 1, []byte{0xc0}, mpt.DefaultLimits(),
	); !errors.Is(err, mpt.ErrUnsupportedProtocolProfile) {
		t.Fatalf("zero profile error = %v", err)
	}
	if _, err := mpt.TypedTransactionValue(
		mpt.BerlinProfile, 0, []byte{0xc0}, mpt.DefaultLimits(),
	); !errors.Is(err, mpt.ErrInvalidEnvelope) {
		t.Fatalf("type zero error = %v", err)
	}
	if _, err := mpt.TypedTransactionValue(
		mpt.BerlinProfile, 1, []byte{0x01}, mpt.DefaultLimits(),
	); !errors.Is(err, mpt.ErrInvalidEnvelope) {
		t.Fatalf("non-list payload error = %v", err)
	}
}

func TestReceiptRootBindsMatchingTransactionTypes(t *testing.T) {
	t.Parallel()

	legacyTransaction, err := mpt.LegacyTransactionValue(
		[]byte{0xc0}, mpt.DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("LegacyTransactionValue() error = %v", err)
	}
	typedTransaction, err := mpt.TypedTransactionValue(
		mpt.LondonProfile, 2, []byte{0xc0}, mpt.DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("TypedTransactionValue() error = %v", err)
	}
	legacyReceipt, err := mpt.LegacyReceiptValue(
		[]byte{0xc0}, mpt.DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("LegacyReceiptValue() error = %v", err)
	}
	typedReceipt, err := mpt.TypedReceiptValue(
		mpt.LondonProfile, 2, []byte{0xc0}, mpt.DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("TypedReceiptValue() error = %v", err)
	}

	transactions := []mpt.EncodedTransactionValue{
		legacyTransaction, typedTransaction,
	}
	receipts := []mpt.EncodedReceiptValue{legacyReceipt, typedReceipt}
	if _, err := mpt.TransactionRoot(
		context.Background(), transactions, mpt.DefaultLimits(),
	); err != nil {
		t.Fatalf("TransactionRoot() error = %v", err)
	}
	if _, err := mpt.ReceiptRoot(
		context.Background(), transactions, receipts, mpt.DefaultLimits(),
	); err != nil {
		t.Fatalf("ReceiptRoot() error = %v", err)
	}

	wrongType, err := mpt.TypedReceiptValue(
		mpt.LondonProfile, 1, []byte{0xc0}, mpt.DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("TypedReceiptValue(wrong type) error = %v", err)
	}
	if _, err := mpt.ReceiptRoot(
		context.Background(),
		transactions,
		[]mpt.EncodedReceiptValue{legacyReceipt, wrongType},
		mpt.DefaultLimits(),
	); !errors.Is(err, mpt.ErrInvalidEnvelope) {
		t.Fatalf("ReceiptRoot(type mismatch) error = %v", err)
	}
	wrongProfile, err := mpt.TypedReceiptValue(
		mpt.ParisProfile, 2, []byte{0xc0}, mpt.DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("TypedReceiptValue(wrong profile) error = %v", err)
	}
	if _, err := mpt.ReceiptRoot(
		context.Background(),
		transactions,
		[]mpt.EncodedReceiptValue{legacyReceipt, wrongProfile},
		mpt.DefaultLimits(),
	); !errors.Is(err, mpt.ErrInvalidEnvelope) {
		t.Fatalf("ReceiptRoot(profile mismatch) error = %v", err)
	}
	typedFirstReceipt, err := mpt.TypedReceiptValue(
		mpt.LondonProfile, 2, []byte{0xc0}, mpt.DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("TypedReceiptValue(kind mismatch) error = %v", err)
	}
	if _, err := mpt.ReceiptRoot(
		context.Background(),
		transactions,
		[]mpt.EncodedReceiptValue{typedFirstReceipt, typedReceipt},
		mpt.DefaultLimits(),
	); !errors.Is(err, mpt.ErrInvalidEnvelope) {
		t.Fatalf("ReceiptRoot(kind mismatch) error = %v", err)
	}
	if _, err := mpt.ReceiptRoot(
		context.Background(), transactions, receipts[:1], mpt.DefaultLimits(),
	); !errors.Is(err, mpt.ErrInvalidEnvelope) {
		t.Fatalf("ReceiptRoot(length mismatch) error = %v", err)
	}

	berlinTransaction, err := mpt.TypedTransactionValue(
		mpt.BerlinProfile, 1, []byte{0xc0}, mpt.DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("TypedTransactionValue(berlin) error = %v", err)
	}
	if _, err := mpt.TransactionRoot(
		context.Background(),
		[]mpt.EncodedTransactionValue{berlinTransaction, typedTransaction},
		mpt.DefaultLimits(),
	); !errors.Is(err, mpt.ErrInvalidEnvelope) {
		t.Fatalf("TransactionRoot(mixed profiles) error = %v", err)
	}
	var zeroTransaction mpt.EncodedTransactionValue
	var zeroReceipt mpt.EncodedReceiptValue
	if _, err := mpt.ReceiptRoot(
		context.Background(),
		[]mpt.EncodedTransactionValue{zeroTransaction},
		[]mpt.EncodedReceiptValue{zeroReceipt},
		mpt.DefaultLimits(),
	); !errors.Is(err, mpt.ErrInvalidEnvelope) {
		t.Fatalf("ReceiptRoot(zero values) error = %v", err)
	}
}
