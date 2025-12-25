package config

import (
	"strings"
	"testing"
)

func TestLoadSecrets(t *testing.T) {
	// Set required environment variables
	t.Setenv(EnvDatabaseURL, "postgres://localhost/test")

	secrets, err := LoadSecrets()
	if err != nil {
		t.Fatalf("LoadSecrets() error: %v", err)
	}

	if secrets.DatabaseURL != "postgres://localhost/test" {
		t.Errorf("DatabaseURL = %s, want postgres://localhost/test", secrets.DatabaseURL)
	}
}

func TestLoadSecretsMissingDatabaseURL(t *testing.T) {
	// t.Setenv with empty value is not sufficient, we need to ensure it's unset
	// but t.Setenv will restore original state after test
	t.Setenv(EnvDatabaseURL, "")

	_, err := LoadSecrets()
	if err == nil {
		t.Error("LoadSecrets() expected error for missing database URL, got nil")
		return
	}

	if !strings.Contains(err.Error(), EnvDatabaseURL) {
		t.Errorf("Error should mention %s, got: %v", EnvDatabaseURL, err)
	}
}

func TestLoadSecretsUICredentials(t *testing.T) {
	t.Setenv(EnvDatabaseURL, "postgres://localhost/test")

	tests := []struct {
		name     string
		username string
		password string
		wantErr  bool
	}{
		{
			name:     "both set",
			username: "admin",
			password: "secret",
			wantErr:  false,
		},
		{
			name:     "neither set",
			username: "",
			password: "",
			wantErr:  false,
		},
		{
			name:     "only username",
			username: "admin",
			password: "",
			wantErr:  true,
		},
		{
			name:     "only password",
			username: "",
			password: "secret",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use t.Setenv which automatically cleans up after the test
			t.Setenv(EnvUIUsername, tt.username)
			t.Setenv(EnvUIPassword, tt.password)

			_, err := LoadSecrets()
			if tt.wantErr && err == nil {
				t.Error("LoadSecrets() expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("LoadSecrets() unexpected error: %v", err)
			}
		})
	}
}

func TestLoadSecretsOptional(t *testing.T) {
	// Set all env vars to empty (t.Setenv cleans up after test)
	t.Setenv(EnvDatabaseURL, "")
	t.Setenv(EnvMasterKey, "")
	t.Setenv(EnvEncryptionKey, "")
	t.Setenv(EnvUIUsername, "")
	t.Setenv(EnvUIPassword, "")

	// Should not error even with missing values
	secrets := LoadSecretsOptional()

	if secrets.DatabaseURL != "" {
		t.Errorf("DatabaseURL should be empty, got %s", secrets.DatabaseURL)
	}
}

func TestSecretsHasUICredentials(t *testing.T) {
	tests := []struct {
		name     string
		username string
		password string
		want     bool
	}{
		{
			name:     "both set",
			username: "admin",
			password: "secret",
			want:     true,
		},
		{
			name:     "neither set",
			username: "",
			password: "",
			want:     false,
		},
		{
			name:     "only username",
			username: "admin",
			password: "",
			want:     false,
		},
		{
			name:     "only password",
			username: "",
			password: "secret",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Secrets{
				UIUsername: tt.username,
				UIPassword: tt.password,
			}
			if got := s.HasUICredentials(); got != tt.want {
				t.Errorf("HasUICredentials() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSecretsString(t *testing.T) {
	s := &Secrets{
		DatabaseURL:   "postgres://localhost/test",
		MasterKey:     "secret-master-key",
		EncryptionKey: "secret-encryption-key",
		UIUsername:    "admin",
		UIPassword:    "password",
	}

	str := s.String()

	// Should not contain actual secrets
	if strings.Contains(str, "postgres://localhost/test") {
		t.Error("String() should not expose database URL")
	}
	if strings.Contains(str, "secret-master-key") {
		t.Error("String() should not expose master key")
	}
	if strings.Contains(str, "secret-encryption-key") {
		t.Error("String() should not expose encryption key")
	}
	if strings.Contains(str, "password") {
		t.Error("String() should not expose UI password")
	}

	// Should contain redacted markers
	if !strings.Contains(str, "[redacted]") {
		t.Error("String() should contain [redacted] markers")
	}
}

func TestSecretsValidateForProduction(t *testing.T) {
	tests := []struct {
		name    string
		secrets *Secrets
		wantErr bool
	}{
		{
			name: "all set",
			secrets: &Secrets{
				DatabaseURL:   "postgres://localhost/test",
				MasterKey:     "master",
				EncryptionKey: "encrypt",
				UIUsername:    "admin",
				UIPassword:    "pass",
			},
			wantErr: false,
		},
		{
			name: "missing master key",
			secrets: &Secrets{
				DatabaseURL:   "postgres://localhost/test",
				EncryptionKey: "encrypt",
				UIUsername:    "admin",
				UIPassword:    "pass",
			},
			wantErr: true,
		},
		{
			name: "missing encryption key",
			secrets: &Secrets{
				DatabaseURL: "postgres://localhost/test",
				MasterKey:   "master",
				UIUsername:  "admin",
				UIPassword:  "pass",
			},
			wantErr: true,
		},
		{
			name: "missing UI credentials",
			secrets: &Secrets{
				DatabaseURL:   "postgres://localhost/test",
				MasterKey:     "master",
				EncryptionKey: "encrypt",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.secrets.ValidateForProduction()
			if tt.wantErr && err == nil {
				t.Error("ValidateForProduction() expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("ValidateForProduction() unexpected error: %v", err)
			}
		})
	}
}
