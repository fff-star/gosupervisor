package config

import (
	"strings"
	"testing"
)

func TestValidateConfig_MissingCommand(t *testing.T) {
	cfg := &Config{
		Programs: map[string]*ProgramConfig{
			"test": {Name: "test", Command: ""},
		},
	}
	warnings := cfg.ValidateConfig()

	found := false
	for _, w := range warnings {
		if strings.Contains(w, "缺少 command") {
			found = true
		}
	}
	if !found {
		t.Error("expected warning about missing command")
	}
}

func TestValidateConfig_MissingDep(t *testing.T) {
	cfg := &Config{
		Programs: map[string]*ProgramConfig{
			"test": {Name: "test", Command: "echo", DependsOn: []string{"nonexistent"}},
		},
	}
	warnings := cfg.ValidateConfig()

	found := false
	for _, w := range warnings {
		if strings.Contains(w, "依赖了不存在的进程") {
			found = true
		}
	}
	if !found {
		t.Error("expected warning about missing dependency")
	}
}

func TestValidateConfig_SelfDep(t *testing.T) {
	cfg := &Config{
		Programs: map[string]*ProgramConfig{
			"test": {Name: "test", Command: "echo", DependsOn: []string{"test"}},
		},
	}
	warnings := cfg.ValidateConfig()

	found := false
	for _, w := range warnings {
		if strings.Contains(w, "不能依赖自身") {
			found = true
		}
	}
	if !found {
		t.Error("expected warning about self-dependency")
	}
}

func TestValidateConfig_Cycle(t *testing.T) {
	cfg := &Config{
		Programs: map[string]*ProgramConfig{
			"a": {Name: "a", Command: "echo", DependsOn: []string{"b"}},
			"b": {Name: "b", Command: "echo", DependsOn: []string{"a"}},
		},
	}
	warnings := cfg.ValidateConfig()

	found := false
	for _, w := range warnings {
		if strings.Contains(w, "循环依赖") {
			found = true
		}
	}
	if !found {
		t.Error("expected cycle detection warning")
	}
}

func TestValidateConfig_InvalidStopSignal(t *testing.T) {
	cfg := &Config{
		Programs: map[string]*ProgramConfig{
			"test": {Name: "test", Command: "echo", StopSignal: "INVALID"},
		},
	}
	warnings := cfg.ValidateConfig()

	found := false
	for _, w := range warnings {
		if strings.Contains(w, "可能无效") {
			found = true
		}
	}
	if !found {
		t.Error("expected warning about invalid stop signal")
	}
}

func TestValidateConfig_ValidStopSignal(t *testing.T) {
	cfg := &Config{
		Programs: map[string]*ProgramConfig{
			"test": {Name: "test", Command: "echo", StopSignal: "SIGTERM"},
		},
	}
	warnings := cfg.ValidateConfig()

	for _, w := range warnings {
		if strings.Contains(w, "可能无效") {
			t.Error("SIGTERM should be valid, got warning:", w)
		}
	}
}

func TestValidateConfig_MissingName(t *testing.T) {
	cfg := &Config{
		Programs: map[string]*ProgramConfig{
			"": {Name: "", Command: "echo"},
		},
	}
	warnings := cfg.ValidateConfig()

	found := false
	for _, w := range warnings {
		if strings.Contains(w, "缺少 name") {
			found = true
		}
	}
	if !found {
		t.Error("expected warning about missing name")
	}
}

func TestValidateConfig_AllValid(t *testing.T) {
	cfg := &Config{
		Programs: map[string]*ProgramConfig{
			"test": {Name: "test", Command: "echo"},
		},
	}
	warnings := cfg.ValidateConfig()
	if len(warnings) > 0 {
		t.Errorf("expected no warnings for valid config, got: %v", warnings)
	}
}

func TestValidateConfig_AllSignals(t *testing.T) {
	// All known signals should not produce warnings
	for sig := range knownSignals {
		cfg := &Config{
			Programs: map[string]*ProgramConfig{
				"test": {Name: "test", Command: "echo", StopSignal: sig},
			},
		}
		warnings := cfg.ValidateConfig()
		for _, w := range warnings {
			if strings.Contains(w, "可能无效") {
				t.Errorf("signal %s should be valid, got warning: %s", sig, w)
			}
		}
	}
}

func TestFindDependencyCycle_NoCycle(t *testing.T) {
	programs := map[string]*ProgramConfig{
		"a": {Name: "a", DependsOn: []string{"b"}},
		"b": {Name: "b", DependsOn: []string{"c"}},
		"c": {Name: "c", DependsOn: []string{}},
	}
	cycle := findDependencyCycle(programs)
	if cycle != "" {
		t.Errorf("expected no cycle, got: %s", cycle)
	}
}

func TestFindDependencyCycle_Simple(t *testing.T) {
	programs := map[string]*ProgramConfig{
		"a": {Name: "a", DependsOn: []string{"b"}},
		"b": {Name: "b", DependsOn: []string{"a"}},
	}
	cycle := findDependencyCycle(programs)
	if cycle == "" {
		t.Error("expected cycle detection")
	}
}

func TestFindDependencyCycle_Indirect(t *testing.T) {
	programs := map[string]*ProgramConfig{
		"a": {Name: "a", DependsOn: []string{"b"}},
		"b": {Name: "b", DependsOn: []string{"c"}},
		"c": {Name: "c", DependsOn: []string{"a"}},
	}
	cycle := findDependencyCycle(programs)
	if cycle == "" {
		t.Error("expected cycle detection for indirect cycle")
	}
}
