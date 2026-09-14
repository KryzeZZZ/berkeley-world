package scene

import "testing"

func TestMatchExactLayerAcceptsUserFacingName(t *testing.T) {
	candidates := []string{"scene-起始城镇", "scene-酒馆", "scene-铁匠铺"}

	for query, want := range map[string]string{
		"铁匠铺":       "scene-铁匠铺",
		"scene-铁匠铺": "scene-铁匠铺",
		"酒馆":        "scene-酒馆",
	} {
		if got := matchExactLayer(query, candidates); got != want {
			t.Errorf("matchExactLayer(%q) = %q, want %q", query, got, want)
		}
	}
}

func TestMatchExactLayerRejectsUnknownName(t *testing.T) {
	if got := matchExactLayer("不存在的地点", []string{"scene-铁匠铺"}); got != "" {
		t.Fatalf("matchExactLayer() = %q, want empty", got)
	}
}
