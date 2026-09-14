package scene

import (
	"fmt"
	"strings"

	"SAO/internal/model"
)

func ensureObjectStateDescription(obj *model.GameObject) {
	if obj == nil {
		return
	}
	if obj.State == nil {
		obj.State = map[string]any{}
	}
	if existing := firstNonEmptyString(obj.State["description"], obj.State["desc"]); existing != "" {
		obj.State["description"] = existing
		return
	}
	name := strings.TrimSpace(obj.Name)
	if name == "" {
		name = obj.ID
	}
	desc := fmt.Sprintf("可交互对象：%s", name)
	if len(obj.Tags) > 0 {
		tags := make([]string, 0, len(obj.Tags))
		for _, tag := range obj.Tags {
			tag = strings.TrimSpace(tag)
			if tag == "" {
				continue
			}
			tags = append(tags, tag)
			if len(tags) >= 3 {
				break
			}
		}
		if len(tags) > 0 {
			desc += "；特征：" + strings.Join(tags, "、")
		}
	}
	obj.State["description"] = desc
}
