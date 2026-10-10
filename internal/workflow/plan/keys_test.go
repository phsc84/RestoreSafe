package plan

import (
	"strings"
	"testing"

	"github.com/phsc84/restoresafe/internal/config"
	"github.com/phsc84/restoresafe/internal/format/catalog"
	"github.com/phsc84/restoresafe/internal/format/container"
	"github.com/phsc84/restoresafe/internal/testutil"
)

func infoWithKeySet(ks *container.KeySet) catalog.SetInfo {
	return catalog.SetInfo{Header: &container.Header{KeySet: *ks, CreatedUTC: ks.CreatedUTC}}
}

func TestKeysFor(t *testing.T) {
	t.Parallel()

	ks, _ := testutil.NewPasswordKeySet(t, []byte("pw"))
	cfg := &config.Config{AuthenticationMode: config.AuthModePassword}
	infos := []catalog.SetInfo{infoWithKeySet(ks)}

	if p := KeysFor(cfg, nil); p.Existing != nil || !strings.Contains(p.NewKeysReason, "No existing keys") {
		t.Fatalf("empty directory: %+v", p)
	}
	if p := KeysFor(cfg, infos); p.Existing == nil || p.Existing.ID != ks.ID {
		t.Fatalf("matching keys not reused: %+v", p)
	}
	for _, tc := range []struct {
		mutate func(*config.Config)
		want   string
	}{
		{func(c *config.Config) { c.AuthenticationMode = config.AuthModePasswordYubiKey }, "Authentication_mode changed"},
		{func(c *config.Config) { c.RecoveryCode = true }, "Recovery_code enabled"},
	} {
		c := *cfg
		tc.mutate(&c)
		if p := KeysFor(&c, infos); p.Existing != nil || !strings.Contains(p.NewKeysReason, tc.want) {
			t.Fatalf("expected new keys with reason %q, got %+v", tc.want, p)
		}
	}
}
