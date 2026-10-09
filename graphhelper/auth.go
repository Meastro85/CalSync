package graphhelper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity/cache"
)

var ErrLoginRequired = errors.New("graphhelper: interactive login required, run the login command as the service account")

var tokenScopes = []string{"https://graph.microsoft.com/Calendars.ReadWrite"}

func newCredential(cfg Config, record azidentity.AuthenticationRecord, silentOnly bool) (*azidentity.DeviceCodeCredential, error) {
	c, err := cache.New(&cache.Options{Name: cfg.CacheName})
	if err != nil {
		return nil, fmt.Errorf("graphhelper: token cache: %w", err)
	}
	return azidentity.NewDeviceCodeCredential(&azidentity.DeviceCodeCredentialOptions{
		TenantID:                       cfg.TenantID,
		ClientID:                       cfg.ClientID,
		Cache:                          c,
		AuthenticationRecord:           record,
		DisableAutomaticAuthentication: silentOnly,
	})
}

func Login(ctx context.Context, cfg Config) error {
	if err := cfg.validateAuth(); err != nil {
		return err
	}
	cred, err := newCredential(cfg, azidentity.AuthenticationRecord{}, false)
	if err != nil {
		return err
	}
	record, err := cred.Authenticate(ctx, &policy.TokenRequestOptions{Scopes: tokenScopes})
	if err != nil {
		return fmt.Errorf("graphhelper: login: %w", err)
	}
	b, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return os.WriteFile(cfg.AuthRecordPath, b, 0o600)
}

func loadRecord(path string) (azidentity.AuthenticationRecord, error) {
	var rec azidentity.AuthenticationRecord
	b, err := os.ReadFile(path)
	if err != nil {
		return rec, fmt.Errorf("%w (reading %s: %v)", ErrLoginRequired, path, err)
	}
	if err := json.Unmarshal(b, &rec); err != nil {
		return rec, fmt.Errorf("graphhelper: parse auth record: %w", err)
	}
	return rec, nil
}
