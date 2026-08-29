package alert

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func keyID(s string) *uuid.UUID { v := uuid.MustParse(s); return &v }
func TestKey_StableAcrossLabelOrder(t *testing.T) {
	r := validRule()
	a := Sample{InstanceID: keyID("11111111-1111-1111-1111-111111111111"), Labels: map[string]string{"b": "2", "a": "1"}}
	b := a
	b.Labels = map[string]string{"a": "1", "b": "2"}
	require.Equal(t, Key(r, a), Key(r, b))
}
func TestKey_DiffersPerInstance(t *testing.T) {
	r := validRule()
	a := Sample{InstanceID: keyID("11111111-1111-1111-1111-111111111111")}
	b := Sample{InstanceID: keyID("22222222-2222-2222-2222-222222222222")}
	require.NotEqual(t, Key(r, a), Key(r, b))
}
func TestKey_ClusterScopeUsesClusterID(t *testing.T) {
	r := validRule()
	r.Scope = ScopeCluster
	c := int64(7)
	require.Contains(t, Key(r, Sample{ClusterID: &c}), "/7/")
}
func TestKey_DatabaseScopeIncludesDatname(t *testing.T) {
	r := validRule()
	r.Scope = ScopeDatabase
	require.Contains(t, Key(r, Sample{InstanceID: keyID("11111111-1111-1111-1111-111111111111"), Datname: "app"}), ":app/")
}
func TestKey_EmptyLabelsProducesTrailingSlash(t *testing.T) {
	require.True(t, len(Key(validRule(), Sample{InstanceID: keyID("11111111-1111-1111-1111-111111111111")})) > 0)
	require.Equal(t, '/', rune(Key(validRule(), Sample{InstanceID: keyID("11111111-1111-1111-1111-111111111111")})[len(Key(validRule(), Sample{InstanceID: keyID("11111111-1111-1111-1111-111111111111")}))-1]))
}
func TestDedupID_IncludesStartedAt(t *testing.T) {
	a := Alert{Key: "k", StartedAt: time.Unix(1, 2)}
	require.Contains(t, DedupID(a), "1970-01-01T00:00:01.000000002Z")
}
func TestDedupID_DiffersAfterRefire(t *testing.T) {
	a := Alert{Key: "k", StartedAt: time.Unix(1, 0)}
	b := a
	b.StartedAt = b.StartedAt.Add(time.Second)
	require.NotEqual(t, DedupID(a), DedupID(b))
}
