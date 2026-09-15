package tool

import (
	"encoding/json"
	"testing"
)

type Profile struct {
	LastLogin int `json:"last_login,omitzero"`
}

type User struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Profile
}

func TestUserSchema(t *testing.T) {
	schema, err := SchemaFor[User]()
	if err != nil {
		t.Fatal(err)
	}

	schemaStr, _ := json.Marshal(schema)
	t.Log(string(schemaStr))
}
