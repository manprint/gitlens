package command

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func validInvalid(s WorkbookSetting) (string, string) {
	switch s.Kind {
	case SettingBool:
		return "off", "false"
	case SettingKB, SettingInt:
		return fmtNum(s.Min), fmtNum(s.Max + 1)
	case SettingDecimal:
		if s.Min >= 1 {
			return "1.5", "0.5"
		}
		return "1.1", "-1"
	default:
		return "force_generic_plan", "generic"
	}
}

func TestWBK001_SettingsTable(t *testing.T) {
	require.Len(t, WorkbookSettings, 32)
	for _, s := range WorkbookSettings {
		good, bad := validInvalid(s)
		require.NoError(t, ValidateSetting(s.Name, good), s.Name)
		err := ValidateSetting(s.Name, bad)
		require.Error(t, err, s.Name)
		require.Contains(t, err.Error(), s.Name)
	}
	require.ErrorContains(t, ValidateSetting("shared_buffers", "1"), "shared_buffers")
	require.ErrorContains(t, ValidateSetting("work_mem", "63"), "work_mem")
	require.ErrorContains(t, ValidateSetting("work_mem", "64kB"), "work_mem")
	require.ErrorContains(t, ValidateSetting("hash_mem_multiplier", "1001"), "hash_mem_multiplier")
	require.ErrorContains(t, ValidateSetting("random_page_cost", "1.1234567"), "random_page_cost")
	require.NoError(t, ValidateSetting("max_parallel_workers_per_gather", "0"))
	require.Error(t, ValidateSetting("max_parallel_workers_per_gather", "9"))

	many := map[string]string{}
	for _, s := range WorkbookSettings[:17] {
		good, _ := validInvalid(s)
		many[s.Name] = good
	}
	require.ErrorContains(t, ValidateSettings(many), "at most 16")
	delete(many, WorkbookSettings[0].Name)
	require.NoError(t, ValidateSettings(many))

	require.Equal(t, "65536kB", SettingValue("work_mem", "65536"))
	require.Equal(t, "8kB", SettingValue("effective_cache_size", "8"))
	require.Equal(t, "off", SettingValue("enable_seqscan", "off"))
	require.Equal(t, "1.1", SettingValue("random_page_cost", "1.1"))
}

func TestWBK002_NoSQLThroughSettingsOrHints(t *testing.T) {
	for _, s := range WorkbookSettings {
		for _, v := range []string{"on; DROP TABLE x", "1; DROP TABLE x", "on'; --", "1'; --"} {
			require.Error(t, ValidateSetting(s.Name, v), "%s=%q", s.Name, v)
		}
	}
	for _, h := range []string{"SeqScan(t) */ SELECT 1", "/* x", "SeqScan(t);", "SeqScan(t)\nIndexScan(u)", "SeqScan('t')"} {
		require.Error(t, ValidateHints(h), "%q", h)
	}
	// The decoder refuses any key that could carry SQL text: unknown fields
	// are an error, so a body with sql or statement never reaches the agent.
	for _, key := range []string{"sql", "statement"} {
		var a Args
		err := json.Unmarshal([]byte(fmt.Sprintf(`{"queryid":"1","settings":{"work_mem":"1024"},%q:"DROP TABLE x"}`, key)), &a)
		require.Error(t, err, key)
	}
	var a Args
	require.NoError(t, json.Unmarshal([]byte(`{"queryid":"1","settings":{"work_mem":"1024"},"hints":"SeqScan(t)"}`), &a))
	require.Equal(t, map[string]string{"work_mem": "1024"}, a.Settings)
	require.Equal(t, "SeqScan(t)", a.Hints)
	require.NoError(t, a.Validate(KindExplain))
	a.Settings["work_mem"] = "1024; DROP TABLE x"
	require.ErrorContains(t, a.Validate(KindExplain), "work_mem")
}

func TestWBK003_Hints(t *testing.T) {
	require.NoError(t, ValidateHints("SeqScan(t) Leading(a b)"))
	require.NoError(t, ValidateHints(`IndexScan(t "Ix" ) Rows(a b #10) Parallel(t 2 hard)`))
	require.NoError(t, ValidateHints(""))
	for _, h := range []string{`Set(work_mem "1GB")`, "set (x y)", "SeqScan(t) SET(enable_seqscan off)"} {
		require.EqualError(t, ValidateHints(h), "use settings instead of a Set hint", h)
	}
	require.NoError(t, ValidateHints(strings.Repeat("a", 1024)))
	require.ErrorContains(t, ValidateHints(strings.Repeat("a", 1025)), "1024")
	require.Error(t, ValidateHints("Rows(a b *10)"), "the * correction form is not available")
}

func TestWBK013_ExplainOnly(t *testing.T) {
	pid := 7
	require.ErrorContains(t, Args{PID: &pid, Settings: map[string]string{"jit": "off"}}.Validate(KindCancel), "settings and hints")
	require.ErrorContains(t, Args{PID: &pid, Hints: "SeqScan(t)"}.Validate(KindCancel), "settings and hints")
	q := int64(1)
	require.ErrorContains(t, Args{QueryID: &q, Datname: "app", IndexSchema: "s", IndexRelation: "r", IndexColumns: []string{"c"}, Hints: "SeqScan(t)"}.Validate(KindIndexCheck), "settings and hints")

	plain := Args{QueryID: &q, Datname: "app"}
	body, err := json.Marshal(plain)
	require.NoError(t, err)
	require.Equal(t, `{"queryid":1,"datname":"app"}`, string(body))
	require.NotContains(t, string(body), "settings")
	require.NotContains(t, string(body), "hints")
}
