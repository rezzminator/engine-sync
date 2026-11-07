package rewrite

import (
	"testing"
)

func TestTextRewritesRespectModelAndCommandBoundaries(t *testing.T) {
	if got := ModelsIn("tiny tiny-extra", map[string]string{"tiny": "chosen"}); got != "chosen tiny-extra" {
		t.Fatal(got)
	}
	if got := CommandsIn("/dev run /dev/tty", map[string]string{"dev": "flat"}, func(s string) string { return "$" + s }); got != "$flat run /dev/tty" {
		t.Fatal(got)
	}
}

// TestCommandPrefixKeepsAnUnreadablePluginsNames: a "{plugin}:" key flattens
// every command of a plugin whose records could not be read.
func TestCommandPrefixKeepsAnUnreadablePluginsNames(t *testing.T) {
	input := "/broken:doctor /broken:a:b /broken: /broken:x/y /tidy"
	want := "$broken-doctor $broken-a-b /broken: /broken:x/y $tidy"
	if got := CommandsIn(input, map[string]string{"broken:": "broken-", "tidy": "tidy"}, func(s string) string { return "$" + s }); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestReviewCommandReferencesPreserveFilenames(t *testing.T) {
	input := "{clone}/inspect.config.json /inspect. /inspect/file /inspect run"
	want := "{clone}/inspect.config.json $inspect. /inspect/file $inspect run"
	if got := CommandsIn(input, map[string]string{"inspect": "$inspect"}, nil); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestAgentRosterRewritesWholePluginAgentIds(t *testing.T) {
	roster := map[string]string{"flights:lander": "flights-lander", "flights:foreman": "flights-foreman", "flights:foreman-sonnet": "flights-foreman-sonnet"}
	for _, tc := range []struct{ name, text, want string }{
		{"whole id", "spawn flights:lander now", "spawn flights-lander now"},
		{"quoted and at the edges", "flights:lander `flights:foreman` \"flights:foreman-sonnet\"", "flights-lander `flights-foreman` \"flights-foreman-sonnet\""},
		{"longer id keeps its own twin", "flights:foreman-sonnet, then flights:foreman.", "flights-foreman-sonnet, then flights-foreman."},
		{"name continues past the id", "flights:lander-x flights:landers", "flights:lander-x flights:landers"},
		{"file name or command", "flights:lander.md /flights:lander x/flights:lander", "flights:lander.md /flights:lander x/flights:lander"},
		{"id inside another id", "other-flights:lander my:flights:lander", "other-flights:lander my:flights:lander"},
		{"no roster", "flights:lander", "flights:lander"},
		{"unreadable plugin prefix", "broken:lander broken:a:b broken: broken:x.md broken:x/y", "broken-lander broken-a-b broken: broken:x.md broken:x/y"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agents := roster
			switch tc.name {
			case "no roster":
				agents = nil
			case "unreadable plugin prefix":
				agents = map[string]string{"broken:": "broken-"}
			}
			if got := AgentsIn(tc.text, agents); got != tc.want {
				t.Fatalf("AgentsIn(%q) = %q, want %q", tc.text, got, tc.want)
			}
		})
	}
}

func TestPluginRootReplacesEveryReference(t *testing.T) {
	for _, tc := range []struct{ name, text, root, want string }{
		{"every reference", "python3 ${CLAUDE_PLUGIN_ROOT}/skills/a.py ${CLAUDE_PLUGIN_ROOT}", "/plugins/flights", "python3 /plugins/flights/skills/a.py /plugins/flights"},
		{"other variables stay", "$CLAUDE_PLUGIN_ROOT ${CLAUDE_PLUGIN_DATA}", "/plugins/flights", "$CLAUDE_PLUGIN_ROOT ${CLAUDE_PLUGIN_DATA}"},
		{"no root", "${CLAUDE_PLUGIN_ROOT}/x", "", "${CLAUDE_PLUGIN_ROOT}/x"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := PluginRootIn(tc.text, tc.root); got != tc.want {
				t.Fatalf("PluginRootIn(%q, %q) = %q, want %q", tc.text, tc.root, got, tc.want)
			}
		})
	}
}
