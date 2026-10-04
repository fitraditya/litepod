package docker

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fitraditya/litepod/internal/domain"
)

func TestPullAuth(t *testing.T) {
	r := (&Repository{}).WithRegistryAuth(map[string]RegistryCredential{
		"ghcr.io":                   {"gh", "ghp"},
		"registry.example.com:5000": {"bot", "secret"},
		"docker.io":                 {"hub", "tok"},
	})

	decode := func(s string) map[string]string {
		raw, err := base64.URLEncoding.DecodeString(s)
		require.NoError(t, err)
		var m map[string]string
		require.NoError(t, json.Unmarshal(raw, &m))
		return m
	}

	cases := []struct {
		image, user, server string
	}{
		{"ghcr.io/acme/app:v1", "gh", "ghcr.io"},
		{"registry.example.com:5000/team/app", "bot", "registry.example.com:5000"},
		{"redis:7-alpine", "hub", "docker.io"},
		{"acme/app:v1", "hub", "docker.io"},
	}
	for _, c := range cases {
		t.Run(c.image, func(t *testing.T) {
			auth, err := r.pullAuth(c.image, nil)
			require.NoError(t, err)
			require.NotEmpty(t, auth)
			m := decode(auth)
			assert.Equal(t, c.user, m["username"])
			assert.Equal(t, c.server, m["serveraddress"])
		})
	}

	t.Run("unconfigured host is anonymous", func(t *testing.T) {
		auth, err := r.pullAuth("quay.io/org/app:1", nil)
		require.NoError(t, err)
		assert.Empty(t, auth)
	})
	t.Run("no creds configured", func(t *testing.T) {
		auth, err := (&Repository{}).pullAuth("ghcr.io/acme/app", nil)
		require.NoError(t, err)
		assert.Empty(t, auth)
	})
	t.Run("invalid reference", func(t *testing.T) {
		_, err := r.pullAuth("Not A Ref", nil)
		assert.ErrorIs(t, err, domain.ErrInvalidInput)
	})

	t.Run("override replaces configured credential", func(t *testing.T) {
		auth, err := r.pullAuth("ghcr.io/acme/app:v1", &domain.RegistryAuth{Username: "ci", Password: "tok"})
		require.NoError(t, err)
		m := decode(auth)
		assert.Equal(t, "ci", m["username"])
		assert.Equal(t, "tok", m["password"])
		assert.Equal(t, "ghcr.io", m["serveraddress"])
	})
	t.Run("override works with no node credentials", func(t *testing.T) {
		auth, err := (&Repository{}).pullAuth("registry.example.com/x/y", &domain.RegistryAuth{Username: "u", Password: "p"})
		require.NoError(t, err)
		assert.Equal(t, "registry.example.com", decode(auth)["serveraddress"])
	})
}
