package command

import (
	"fmt"
	"regexp"
	"strconv"
)

// MaxSettings bounds the planner settings of one explain variant.
const MaxSettings = 16

// MaxHintsBytes bounds the pg_hint_plan hint text of one explain variant.
const MaxHintsBytes = 1024

// WorkbookSetting is one planner setting a tuning-workbook variant may change.
// Kind names the value rule; Unit is appended to the value the agent passes to
// set_config.
type WorkbookSetting struct {
	Name  string
	Kind  string
	Min   float64
	Max   float64
	Unit  string
	check func(s WorkbookSetting, value string) error
}

// Value rule names, also used in the OpenAPI description.
const (
	SettingBool    = "bool"
	SettingKB      = "kb"
	SettingDecimal = "decimal"
	SettingInt     = "int"
	SettingEnum    = "enum"
)

var (
	digitsRe  = regexp.MustCompile(`^[0-9]{1,10}$`)
	decimalRe = regexp.MustCompile(`^[0-9]{1,4}(\.[0-9]{1,6})?$`)
	hintsRe   = regexp.MustCompile(`^[A-Za-z0-9 _,()#+\-."]*$`)
	setHintRe = regexp.MustCompile(`(?i)\bset\s*\(`)
)

func boolSetting(name string) WorkbookSetting {
	return WorkbookSetting{Name: name, Kind: SettingBool, check: checkBool}
}

func checkBool(_ WorkbookSetting, v string) error {
	if v != "on" && v != "off" {
		return fmt.Errorf("must be on or off")
	}
	return nil
}

func checkRange(s WorkbookSetting, v string, re *regexp.Regexp) error {
	if !re.MatchString(v) {
		return fmt.Errorf("must be a number")
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil || n < s.Min || n > s.Max {
		return fmt.Errorf("must be between %s and %s", fmtNum(s.Min), fmtNum(s.Max))
	}
	return nil
}

func fmtNum(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

func checkWhole(s WorkbookSetting, v string) error { return checkRange(s, v, digitsRe) }

func checkDecimal(s WorkbookSetting, v string) error { return checkRange(s, v, decimalRe) }

func checkPlanCacheMode(_ WorkbookSetting, v string) error {
	switch v {
	case "auto", "force_generic_plan", "force_custom_plan":
		return nil
	}
	return fmt.Errorf("must be auto, force_generic_plan or force_custom_plan")
}

// WorkbookSettings is the closed list of settings a variant may change. The
// server, the agent and the console (through the OpenAPI enum) use these
// names; anything else is refused.
var WorkbookSettings = func() []WorkbookSetting {
	var out []WorkbookSetting
	for _, n := range []string{
		"enable_seqscan", "enable_indexscan", "enable_indexonlyscan", "enable_bitmapscan",
		"enable_tidscan", "enable_nestloop", "enable_hashjoin", "enable_mergejoin",
		"enable_hashagg", "enable_sort", "enable_incremental_sort", "enable_material",
		"enable_memoize", "enable_gathermerge", "enable_parallel_append", "enable_parallel_hash",
		"enable_partition_pruning", "enable_partitionwise_join", "enable_partitionwise_aggregate", "jit",
	} {
		out = append(out, boolSetting(n))
	}
	out = append(out,
		WorkbookSetting{Name: "work_mem", Kind: SettingKB, Min: 64, Max: 262144, Unit: "kB", check: checkWhole},
		WorkbookSetting{Name: "effective_cache_size", Kind: SettingKB, Min: 8, Max: 1073741824, Unit: "kB", check: checkWhole},
	)
	for _, n := range []string{"random_page_cost", "seq_page_cost", "cpu_tuple_cost", "cpu_index_tuple_cost", "cpu_operator_cost"} {
		out = append(out, WorkbookSetting{Name: n, Kind: SettingDecimal, Min: 0, Max: 9999.999999, check: checkDecimal})
	}
	out = append(out,
		WorkbookSetting{Name: "hash_mem_multiplier", Kind: SettingDecimal, Min: 1, Max: 1000, check: checkDecimal},
		WorkbookSetting{Name: "join_collapse_limit", Kind: SettingInt, Min: 1, Max: 100, check: checkWhole},
		WorkbookSetting{Name: "from_collapse_limit", Kind: SettingInt, Min: 1, Max: 100, check: checkWhole},
		WorkbookSetting{Name: "max_parallel_workers_per_gather", Kind: SettingInt, Min: 0, Max: 8, check: checkWhole},
		WorkbookSetting{Name: "plan_cache_mode", Kind: SettingEnum, check: checkPlanCacheMode},
	)
	return out
}()

var workbookSettingByName = func() map[string]WorkbookSetting {
	m := make(map[string]WorkbookSetting, len(WorkbookSettings))
	for _, s := range WorkbookSettings {
		m[s.Name] = s
	}
	return m
}()

// ValidateSetting checks one setting name and value against WorkbookSettings.
func ValidateSetting(name, value string) error {
	s, ok := workbookSettingByName[name]
	if !ok {
		return fmt.Errorf("setting %q is not one a variant may change", name)
	}
	if err := s.check(s, value); err != nil {
		return fmt.Errorf("setting %s: %w", name, err)
	}
	return nil
}

// ValidateSettings checks a whole settings map.
func ValidateSettings(settings map[string]string) error {
	if len(settings) > MaxSettings {
		return fmt.Errorf("settings takes at most %d entries", MaxSettings)
	}
	for name, value := range settings {
		if err := ValidateSetting(name, value); err != nil {
			return err
		}
	}
	return nil
}

// SettingValue is the value the agent passes to set_config: the size settings
// get their kB unit. The pair must have passed ValidateSetting.
func SettingValue(name, value string) string {
	return value + workbookSettingByName[name].Unit
}

// ValidateHints checks a pg_hint_plan hint text. Only letters, digits, space
// and _ , ( ) # + - . " are allowed, so the text can never close the comment
// it is placed in; a Set hint is refused because it would bypass the settings
// list.
func ValidateHints(h string) error {
	if len(h) > MaxHintsBytes {
		return fmt.Errorf("hints is longer than %d bytes", MaxHintsBytes)
	}
	if !hintsRe.MatchString(h) {
		return fmt.Errorf("hints may contain only letters, digits, space and _ , ( ) # + - . \"")
	}
	if setHintRe.MatchString(h) {
		return fmt.Errorf("use settings instead of a Set hint")
	}
	return nil
}
