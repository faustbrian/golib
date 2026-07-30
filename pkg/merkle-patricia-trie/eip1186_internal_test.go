package mpt

import (
	"context"
	"errors"
	"testing"
)

func TestStorageProofSetValidation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	limits := DefaultLimits()
	account := Account{storageRoot: EmptyRoot(), verified: true}
	var firstSlot [RootBytes]byte
	firstSlot[RootBytes-1] = 1
	var secondSlot [RootBytes]byte
	secondSlot[RootBytes-1] = 2
	var thirdSlot [RootBytes]byte
	thirdSlot[RootBytes-1] = 3
	absence := StorageAbsenceClaim(firstSlot, Proof{})

	if err := VerifyStorageProofs(
		ctx, account, []StorageProofClaim{absence}, limits,
	); err != nil {
		t.Fatalf("VerifyStorageProofs(absence) error = %v", err)
	}
	exactClaimLimit := limits
	exactClaimLimit.MaxProofKeys = 1
	exactClaimLimit.MaxHashOperations = 1
	if err := VerifyStorageProofs(
		ctx, account, []StorageProofClaim{absence}, exactClaimLimit,
	); err != nil {
		t.Fatalf("VerifyStorageProofs(exact claim limits) error = %v", err)
	}

	invalidLimits := limits
	invalidLimits.MaxProofKeys = 0
	tests := []struct {
		name    string
		ctx     context.Context
		account Account
		claims  []StorageProofClaim
		limits  Limits
		want    error
	}{
		{
			name: "invalid limits", ctx: ctx, account: account,
			claims: []StorageProofClaim{absence}, limits: invalidLimits,
			want: ErrResourceLimit,
		},
		{
			name: "nil context", account: account,
			claims: []StorageProofClaim{absence}, limits: limits,
			want: ErrInvalidContext,
		},
		{
			name: "unverified account", ctx: ctx,
			claims: []StorageProofClaim{absence}, limits: limits,
			want: ErrInvalidAccount,
		},
		{
			name: "empty set", ctx: ctx, account: account,
			limits: limits, want: ErrInvalidProofClaim,
		},
		{
			name: "zero claim", ctx: ctx, account: account,
			claims: []StorageProofClaim{{}}, limits: limits,
			want: ErrInvalidProofClaim,
		},
		{
			name: "duplicate slot", ctx: ctx, account: account,
			claims: []StorageProofClaim{
				absence, StorageAbsenceClaim(firstSlot, Proof{}),
			},
			limits: limits, want: ErrDuplicateProofKey,
		},
		{
			name: "empty membership value", ctx: ctx, account: account,
			claims: []StorageProofClaim{
				StorageMembershipClaim(firstSlot, nil, Proof{}),
			},
			limits: limits, want: ErrInvalidStorageValue,
		},
		{
			name: "leading-zero membership value", ctx: ctx, account: account,
			claims: []StorageProofClaim{
				StorageMembershipClaim(firstSlot, []byte{0, 1}, Proof{}),
			},
			limits: limits, want: ErrInvalidStorageValue,
		},
		{
			name: "oversized membership value", ctx: ctx, account: account,
			claims: []StorageProofClaim{
				StorageMembershipClaim(
					firstSlot, make([]byte, RootBytes+1), Proof{},
				),
			},
			limits: limits, want: ErrInvalidStorageValue,
		},
		{
			name: "maximum membership value", ctx: ctx, account: account,
			claims: []StorageProofClaim{
				StorageMembershipClaim(
					firstSlot,
					append([]byte{1}, make([]byte, RootBytes-1)...),
					Proof{},
				),
			},
			limits: limits, want: ErrFailedProof,
		},
		{
			name: "failed membership", ctx: ctx, account: account,
			claims: []StorageProofClaim{
				StorageMembershipClaim(firstSlot, []byte{1}, Proof{}),
			},
			limits: limits, want: ErrFailedProof,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := VerifyStorageProofs(
				test.ctx, test.account, test.claims, test.limits,
			)
			if !errors.Is(err, test.want) {
				t.Fatalf("VerifyStorageProofs() error = %v, want %v", err, test.want)
			}
		})
	}

	tooMany := limits
	tooMany.MaxProofKeys = 1
	if err := VerifyStorageProofs(
		ctx,
		account,
		[]StorageProofClaim{
			absence, StorageAbsenceClaim(secondSlot, Proof{}),
		},
		tooMany,
	); !errors.Is(err, ErrInvalidProofClaim) {
		t.Fatalf("VerifyStorageProofs(too many) error = %v", err)
	}

	nodeLimited := limits
	nodeLimited.MaxProofNodes = 2
	if err := VerifyStorageProofs(
		ctx,
		account,
		[]StorageProofClaim{
			StorageAbsenceClaim(firstSlot, Proof{nodes: [][]byte{{0x80}}}),
			StorageAbsenceClaim(secondSlot, Proof{nodes: [][]byte{{0x80}}}),
			StorageAbsenceClaim(thirdSlot, Proof{nodes: [][]byte{{0x80}}}),
		},
		nodeLimited,
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("VerifyStorageProofs(node limit) error = %v", err)
	}
	exactNodeLimit := limits
	exactNodeLimit.MaxProofNodes = 2
	if err := VerifyStorageProofs(
		ctx,
		account,
		[]StorageProofClaim{
			StorageAbsenceClaim(
				firstSlot,
				Proof{nodes: [][]byte{{0x80}, {0x80}}},
			),
		},
		exactNodeLimit,
	); !errors.Is(err, ErrMalformedProof) {
		t.Fatalf("VerifyStorageProofs(exact node limit) error = %v", err)
	}

	byteLimited := limits
	byteLimited.MaxProofBytes = 2
	if err := VerifyStorageProofs(
		ctx,
		account,
		[]StorageProofClaim{
			StorageAbsenceClaim(firstSlot, Proof{nodes: [][]byte{{0x80}}}),
			StorageAbsenceClaim(secondSlot, Proof{nodes: [][]byte{{0x80}}}),
			StorageAbsenceClaim(thirdSlot, Proof{nodes: [][]byte{{0x80}}}),
		},
		byteLimited,
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("VerifyStorageProofs(byte limit) error = %v", err)
	}
	exactByteLimit := limits
	exactByteLimit.MaxProofBytes = 2
	if err := VerifyStorageProofs(
		ctx,
		account,
		[]StorageProofClaim{
			StorageAbsenceClaim(
				firstSlot,
				Proof{nodes: [][]byte{{0x80, 0x80}}},
			),
		},
		exactByteLimit,
	); !errors.Is(err, ErrMalformedProof) {
		t.Fatalf("VerifyStorageProofs(exact byte limit) error = %v", err)
	}

	hashLimited := limits
	hashLimited.MaxHashOperations = 2
	if err := VerifyStorageProofs(
		ctx,
		account,
		[]StorageProofClaim{
			absence,
			StorageAbsenceClaim(secondSlot, Proof{}),
			StorageAbsenceClaim(thirdSlot, Proof{}),
		},
		hashLimited,
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("VerifyStorageProofs(hash limit) error = %v", err)
	}
}
