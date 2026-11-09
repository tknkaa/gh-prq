package quiz

import (
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/tknkaa/gh-prq/internal/lang"
)

const tfSrc = `resource "azurerm_monitor_action_group" "alert" {
  name                = "ag-${var.env}-alert"
  resource_group_name = azurerm_resource_group.main.name
  enabled             = var.env == "prod" ? true : false
  short_name          = "alert"

  tags = merge(local.tags, { role = "alert" })

  dynamic "email_receiver" {
    for_each = toset(var.alert_emails)
    content {
      name                    = "email-${email_receiver.key}"
      email_address           = email_receiver.value
      use_common_alert_schema = true
    }
  }
}
`

func TestHCLCandidates(t *testing.T) {
	l := lang.ForFile("main.tf")
	if l == nil || !l.IsHCL() {
		t.Fatal(".tf is not HCL")
	}
	src := []byte(tfSrc)
	easy := FindCandidates("main.tf", l, src, allRows(tfSrc), Easy)
	var texts []string
	for _, c := range easy {
		texts = append(texts, c.Kind+": "+string(src[c.Start:c.End]))
	}
	got := strings.Join(texts, "\n")
	for _, want := range []string{
		"reference: azurerm_resource_group.main.name",
		"conditional: var.env == \"prod\" ? true : false",
		"function call: toset(var.alert_emails)",
		"reference: email_receiver.value",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing easy candidate %q in\n%s", want, got)
		}
	}
	// true / false は当て推量になるので出さない
	if strings.Contains(got, ": true\n") || strings.HasSuffix(got, ": true") {
		t.Errorf("bool literal should not be a blank:\n%s", got)
	}

	hard := FindCandidates("main.tf", l, src, allRows(tfSrc), Hard)
	var kinds []string
	for _, c := range hard {
		kinds = append(kinds, c.Kind)
	}
	if !strings.Contains(strings.Join(kinds, ","), "content block") {
		t.Errorf("hard kinds: %v", kinds)
	}
	for _, c := range hard {
		// ブロックの見出し行や波括弧は残す
		if strings.Contains(string(src[c.Start:c.End]), "{\n") && strings.Contains(string(src[c.Start:c.End]), "dynamic") {
			t.Errorf("hard blank includes a block header: %q", src[c.Start:c.End])
		}
	}
}

func TestHCLRoundTrip(t *testing.T) {
	l := lang.ForFile("main.tf")
	src := []byte(tfSrc)
	for _, lv := range []Level{Easy, Normal, Hard} {
		cands := FindCandidates("main.tf", l, src, allRows(tfSrc), lv)
		if len(cands) == 0 {
			t.Fatalf("%s: no candidates", lv)
		}
		picked := Select(cands, 10, rand.New(rand.NewPCG(3, 4)))
		blanked, blanks := Build("main.tf", l, src, picked, 1)
		for i := range blanks {
			b := &blanks[i]
			var filled string
			if b.LineLevel {
				filled = replaceLine(string(blanked), b.Placeholder, b.Expected)
			} else {
				filled = strings.Replace(string(blanked), b.Placeholder, b.Expected, 1)
			}
			ex := Extract([]byte(filled), b)
			if !ex.Answered || !Grade(l, src, b, ex.Answer) {
				t.Errorf("%s #%d (%s): correct answer not accepted: %+v", lv, b.ID, b.Kind, ex)
			}
			wrong := strings.Replace(filled, strings.TrimSpace(b.Expected), "var.wrong", 1)
			if ex := Extract([]byte(wrong), b); ex.Answered && Grade(l, src, b, ex.Answer) {
				t.Errorf("%s #%d: wrong answer accepted", lv, b.ID)
			}
		}
	}
}
