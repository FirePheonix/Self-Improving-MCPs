package improvementsandbox

import "testing"

func TestValidateRejectsEmpty(t *testing.T) {
	if err := Validate(Overlay{}); err == nil {
		t.Fatal("expected error")
	}
}

func TestValidateOK(t *testing.T) {
	desc := "better"
	err := Validate(Overlay{
		ToolPatches: map[string]ToolPatch{"echo": {Description: &desc}},
	})
	if err != nil {
		t.Fatal(err)
	}
}
