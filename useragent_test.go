package main

import (
	"regexp"
	"strings"
	"testing"
)

// TestProfilesCoherent — каждый профиль должен быть внутренне согласован:
// major-версия из UA присутствует в sec-ch-ua, а платформа sec-ch-ua-platform
// соответствует ОС в UA. Иначе WAF сразу видит подделку.
func TestProfilesCoherent(t *testing.T) {
	if len(browserProfiles) != 10 {
		t.Fatalf("ожидалось 10 профилей, а их %d", len(browserProfiles))
	}
	verRe := regexp.MustCompile(`Chrome/(\d+)\.`)
	for i, p := range browserProfiles {
		m := verRe.FindStringSubmatch(p.ua)
		if m == nil {
			t.Errorf("профиль %d: в UA не найдена версия Chrome: %q", i, p.ua)
			continue
		}
		major := m[1]
		if !strings.Contains(p.secCHUA, `v="`+major+`"`) {
			t.Errorf("профиль %d: версия %s из UA отсутствует в sec-ch-ua %q", i, major, p.secCHUA)
		}
		want := map[string]string{
			"Mac OS X":     `"macOS"`,
			"Windows NT":   `"Windows"`,
			"Linux x86_64": `"Linux"`,
		}
		matched := false
		for uaMark, plat := range want {
			if strings.Contains(p.ua, uaMark) {
				matched = true
				if p.platform != plat {
					t.Errorf("профиль %d: ОС %q в UA, а platform=%s", i, uaMark, p.platform)
				}
			}
		}
		if !matched {
			t.Errorf("профиль %d: не удалось определить ОС по UA: %q", i, p.ua)
		}
	}
}

// TestPickProfileInPool — pickProfile всегда возвращает элемент пула.
func TestPickProfileInPool(t *testing.T) {
	for i := 0; i < 50; i++ {
		p := pickProfile()
		found := false
		for _, bp := range browserProfiles {
			if bp.ua == p.ua && bp.secCHUA == p.secCHUA {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("pickProfile вернул профиль не из пула: %+v", p)
		}
	}
}
