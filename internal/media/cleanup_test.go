package media

import "testing"

func TestConfigureCleanupRulesValidatesIdentifiers(t *testing.T) {
	service := &Service{}
	if err := service.ConfigureCleanupRules([]ReferenceRule{{ID: "user_avatar", Table: "users", Column: "avatar_media_id"}}); err != nil {
		t.Fatal(err)
	}
	if rules := service.ReferenceRules(); len(rules) != 1 || rules[0].Label != "user_avatar" {
		t.Fatalf("rules = %#v", rules)
	}
	for _, rule := range []ReferenceRule{
		{ID: "bad_table", Table: "users; DROP TABLE users", Column: "avatar_media_id"},
		{ID: "bad_column", Table: "users", Column: "avatar-media-id"},
	} {
		if err := service.ConfigureCleanupRules([]ReferenceRule{rule}); err == nil {
			t.Fatalf("unsafe rule accepted: %#v", rule)
		}
	}
}

func TestConfigureCleanupRulesRejectsDuplicateIDs(t *testing.T) {
	service := &Service{}
	err := service.ConfigureCleanupRules([]ReferenceRule{
		{ID: "image", Table: "users", Column: "avatar_media_id"},
		{ID: "image", Table: "posts", Column: "image_media_id"},
	})
	if err == nil {
		t.Fatal("duplicate rule ID accepted")
	}
}
