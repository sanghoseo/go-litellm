//go:build ignore

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

type flexibleNumber struct {
	value *float64
}

func (number *flexibleNumber) UnmarshalJSON(raw []byte) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		number.value = nil
		return nil
	}
	parsed, err := strconv.ParseFloat(string(trimmed), 64)
	if err != nil {
		number.value = nil
		return nil
	}
	number.value = &parsed
	return nil
}

type modelPrice struct {
	InputCost   flexibleNumber `json:"input_cost_per_token"`
	OutputCost  flexibleNumber `json:"output_cost_per_token"`
	BatchInput  flexibleNumber `json:"input_cost_per_token_batches"`
	BatchOutput flexibleNumber `json:"output_cost_per_token_batches"`
	CacheRead   flexibleNumber `json:"cache_read_input_token_cost"`
	MaxInput    flexibleNumber `json:"max_input_tokens"`
	MaxOutput   flexibleNumber `json:"max_output_tokens"`
}

func valueOf(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}

type priceSet struct {
	Input     float64
	Output    float64
	BatchIn   float64
	BatchOut  float64
	CacheRead float64
	MaxIn     float64
	MaxOut    float64
}

func main() {
	_, self, _, _ := runtime.Caller(0)
	root := findRepoRoot(filepath.Dir(self))
	raw, err := os.ReadFile(filepath.Join(root, "model_prices_and_context_window.json"))
	if err != nil {
		panic(err)
	}
	var prices map[string]modelPrice
	if err := json.Unmarshal(raw, &prices); err != nil {
		panic(err)
	}

	names := make([]string, 0, len(prices))
	sets := make(map[string]priceSet, len(prices))
	for name, price := range prices {
		if price.InputCost.value == nil && price.OutputCost.value == nil {
			continue
		}
		sets[name] = priceSet{
			Input:     valueOf(price.InputCost.value),
			Output:    valueOf(price.OutputCost.value),
			BatchIn:   valueOf(price.BatchInput.value),
			BatchOut:  valueOf(price.BatchOutput.value),
			CacheRead: valueOf(price.CacheRead.value),
			MaxIn:     valueOf(price.MaxInput.value),
			MaxOut:    valueOf(price.MaxOutput.value),
		}
		names = append(names, name)
	}
	sort.Strings(names)

	var builder strings.Builder
	builder.WriteString("package usage\n\n")
	builder.WriteString("// Code generated from model_prices_and_context_window.json. DO NOT EDIT.\n\n")
	builder.WriteString("const (\n")
	builder.WriteString("\tinputCostIndex    = 0\n")
	builder.WriteString("\toutputCostIndex   = 1\n")
	builder.WriteString("\tbatchInputIndex   = 2\n")
	builder.WriteString("\tbatchOutputIndex  = 3\n")
	builder.WriteString("\tcacheReadIndex    = 4\n")
	builder.WriteString("\tmaxInputIndex     = 5\n")
	builder.WriteString("\tmaxOutputIndex    = 6\n")
	builder.WriteString("\tcostFieldCount    = 7\n\n")
	builder.WriteString(")\n\n")
	builder.WriteString("type modelCostRow struct {\n")
	builder.WriteString("\tname string\n")
	builder.WriteString("\tcost [costFieldCount]float64\n")
	builder.WriteString("}\n\n")
	builder.WriteString(fmt.Sprintf("var modelCostTable = []modelCostRow{\n"))
	for _, name := range names {
		set := sets[name]
		builder.WriteString(fmt.Sprintf("\t{name: %q, cost: [costFieldCount]float64{%.9g, %.9g, %.9g, %.9g, %.9g, %.9g, %.9g}},\n",
			name, set.Input, set.Output, set.BatchIn, set.BatchOut, set.CacheRead, set.MaxIn, set.MaxOut))
	}
	builder.WriteString("}\n\n")
	builder.WriteString("func lookupModelCost(name string) ([costFieldCount]float64, bool) {\n")
	builder.WriteString("\tlow := 0\n")
	builder.WriteString("\thigh := len(modelCostTable) - 1\n")
	builder.WriteString("\tfor low <= high {\n")
	builder.WriteString("\t\tmid := (low + high) / 2\n")
	builder.WriteString("\t\tswitch {\n")
	builder.WriteString("\t\tcase modelCostTable[mid].name < name:\n")
	builder.WriteString("\t\t\tlow = mid + 1\n")
	builder.WriteString("\t\tcase modelCostTable[mid].name > name:\n")
	builder.WriteString("\t\t\thigh = mid - 1\n")
	builder.WriteString("\t\tdefault:\n")
	builder.WriteString("\t\t\treturn modelCostTable[mid].cost, true\n")
	builder.WriteString("\t\t}\n")
	builder.WriteString("\t}\n")
	builder.WriteString("\treturn [costFieldCount]float64{}, false\n")
	builder.WriteString("}\n")

	if err := os.WriteFile(filepath.Join(root, "internal", "usage", "cost_table.go"), []byte(builder.String()), 0o644); err != nil {
		panic(err)
	}
	fmt.Fprintf(os.Stderr, "generated %d priced models\n", len(names))
}

func findRepoRoot(start string) string {
	current := start
	for {
		if _, err := os.Stat(filepath.Join(current, "go.mod")); err == nil {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			return start
		}
		current = parent
	}
}
