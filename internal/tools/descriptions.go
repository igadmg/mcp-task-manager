package tools

import (
	"embed"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// descriptionsFS embeds the text every tool exposes to MCP clients, kept out
// of the registration code so it can be edited without touching Go.
//
//go:embed descriptions/*.yaml
var descriptionsFS embed.FS

// toolText is the text one tool's descriptions/<name>.yaml carries: its own
// description plus one entry per parameter description.
type toolText struct {
	name        string
	Description string            `yaml:"description"`
	Params      map[string]string `yaml:"params"`
}

var toolTexts = loadToolTexts()

func loadToolTexts() map[string]toolText {
	entries, err := descriptionsFS.ReadDir("descriptions")
	if err != nil {
		panic(fmt.Sprintf("tools: reading descriptions directory: %v", err))
	}

	texts := make(map[string]toolText, len(entries))
	for _, entry := range entries {
		data, err := descriptionsFS.ReadFile("descriptions/" + entry.Name())
		if err != nil {
			panic(fmt.Sprintf("tools: reading descriptions/%s: %v", entry.Name(), err))
		}

		var t toolText
		if err := yaml.Unmarshal(data, &t); err != nil {
			panic(fmt.Sprintf("tools: parsing descriptions/%s: %v", entry.Name(), err))
		}

		t.name = strings.TrimSuffix(entry.Name(), ".yaml")
		texts[t.name] = t
	}

	return texts
}

// textFor looks a tool's descriptions/<tool>.yaml up, panicking if it wasn't
// embedded — a missing entry is a programming error, not a runtime one.
func textFor(tool string) toolText {
	t, ok := toolTexts[tool]
	if !ok {
		panic(fmt.Sprintf("tools: no descriptions/%s.yaml", tool))
	}

	return t
}

// param returns the description of one of the tool's parameters, panicking
// if descriptions/<tool>.yaml doesn't have it.
func (t toolText) param(name string) string {
	d, ok := t.Params[name]
	if !ok {
		panic(fmt.Sprintf("tools: descriptions/%s.yaml has no param %q", t.name, name))
	}

	return d
}
