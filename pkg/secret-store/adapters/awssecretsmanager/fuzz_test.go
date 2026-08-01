package awssecretsmanager

import (
	"context"
	"testing"
)

func FuzzPutVersionValidation(f *testing.F) {
	valid := validPutVersionRequest()
	f.Add(valid.Name, valid.VersionID, valid.Stage, valid.Value)
	f.Add("", "", "", []byte{})
	f.Add("name with spaces", "short", "stage with spaces", []byte("value"))

	f.Fuzz(func(
		t *testing.T,
		name string,
		versionID string,
		stage string,
		value []byte,
	) {
		store, err := New(&stubClient{}, "")
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}

		_, _ = store.PutVersion(
			context.Background(),
			PutVersionRequest{
				Name:      name,
				VersionID: versionID,
				Stage:     stage,
				Value:     value,
			},
		)
	})
}

func FuzzGetVersionValidation(f *testing.F) {
	valid := validPutVersionRequest()
	f.Add(valid.Name, valid.VersionID)
	f.Add("", "")
	f.Add("arn:aws:secretsmanager:region:account:secret:name", "short")

	f.Fuzz(func(t *testing.T, secretID string, versionID string) {
		store, err := New(&stubClient{}, "")
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}

		_, _ = store.GetVersion(
			context.Background(),
			GetVersionRequest{SecretID: secretID, VersionID: versionID},
		)
	})
}
