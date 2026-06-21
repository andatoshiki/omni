package providers

import "testing"

func TestParseProviderPageCallback(t *testing.T) {
	tests := []struct {
		data string
		page int
		ok   bool
	}{
		{data: "p:0", page: 0, ok: true},
		{data: "p:12", page: 12, ok: true},
		{data: "p:-1"},
		{data: "p:"},
		{data: "p:one"},
		{data: "l:openai:0"},
	}

	for _, test := range tests {
		t.Run(test.data, func(t *testing.T) {
			page, ok := ParseProviderPageCallback(test.data)
			if page != test.page || ok != test.ok {
				t.Fatalf("ParseProviderPageCallback(%q) = (%d, %v), want (%d, %v)", test.data, page, ok, test.page, test.ok)
			}
		})
	}
}

func TestParseModelListCallback(t *testing.T) {
	tests := []struct {
		data     string
		provider string
		page     int
		ok       bool
	}{
		{data: "l:openrouter:0", provider: "openrouter", page: 0, ok: true},
		{data: "l:local:provider:3", provider: "local:provider", page: 3, ok: true},
		{data: "l::0"},
		{data: "l:openai:-1"},
		{data: "l:openai:many"},
		{data: "l:openai"},
		{data: "p:0"},
	}

	for _, test := range tests {
		t.Run(test.data, func(t *testing.T) {
			provider, page, ok := ParseModelListCallback(test.data)
			if provider != test.provider || page != test.page || ok != test.ok {
				t.Fatalf("ParseModelListCallback(%q) = (%q, %d, %v), want (%q, %d, %v)", test.data, provider, page, ok, test.provider, test.page, test.ok)
			}
		})
	}
}

func TestCallbackDataBuilders(t *testing.T) {
	if got := ProviderPageCallbackData(2); got != "p:2" {
		t.Fatalf("ProviderPageCallbackData(2) = %q", got)
	}
	if got := ModelListCallbackData("openrouter", 4); got != "l:openrouter:4" {
		t.Fatalf("ModelListCallbackData() = %q", got)
	}
}

func TestParseModelCallback(t *testing.T) {
	tests := []struct {
		data string
		want ModelID
		ok   bool
	}{
		{data: "m:openrouter:anthropic/claude-sonnet", want: ModelID{Provider: "openrouter", Model: "anthropic/claude-sonnet"}, ok: true},
		{data: "m:openai:"},
		{data: "m::gpt"},
		{data: "p:0"},
	}

	for _, test := range tests {
		t.Run(test.data, func(t *testing.T) {
			got, ok := ParseModelCallback(test.data)
			if got != test.want || ok != test.ok {
				t.Fatalf("ParseModelCallback(%q) = (%#v, %v), want (%#v, %v)", test.data, got, ok, test.want, test.ok)
			}
		})
	}
}
