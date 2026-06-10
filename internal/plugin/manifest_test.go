package plugin

import (
	"testing"
)

func TestParseManifest_Valid(t *testing.T) {
	data := []byte(`{
		"name": "test-plugin",
		"version": "1.0.0",
		"description": "A test plugin",
		"author": {
			"name": "Test Author",
			"email": "test@example.com",
			"url": "https://example.com/author"
		},
		"keywords": ["test", "example"],
		"skills": "./skills/",
		"interface": {
			"displayName": "Test Plugin Display",
			"shortDescription": "A short description",
			"longDescription": "A long description for the test plugin",
			"developerName": "Test Developer",
			"category": "Productivity",
			"capabilities": ["Interactive", "Write"],
			"brandColor": "#3B82F6"
		}
	}`)

	m, err := ParseManifest(data)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if m == nil {
		t.Fatal("expected manifest, got nil")
	}

	// Verify fields
	if m.Name != "test-plugin" {
		t.Errorf("expected name 'test-plugin', got %q", m.Name)
	}
	if m.Version != "1.0.0" {
		t.Errorf("expected version '1.0.0', got %q", m.Version)
	}
	if m.Description != "A test plugin" {
		t.Errorf("expected description 'A test plugin', got %q", m.Description)
	}
	if m.Author.Name != "Test Author" {
		t.Errorf("expected author name 'Test Author', got %q", m.Author.Name)
	}
	if m.Author.Email != "test@example.com" {
		t.Errorf("expected author email 'test@example.com', got %q", m.Author.Email)
	}
	if m.Skills != "./skills/" {
		t.Errorf("expected skills './skills/', got %q", m.Skills)
	}
	if len(m.Keywords) != 2 {
		t.Errorf("expected 2 keywords, got %d", len(m.Keywords))
	}
	if m.Interface == nil {
		t.Fatal("expected interface, got nil")
	}
	if m.Interface.DisplayName != "Test Plugin Display" {
		t.Errorf("expected displayName 'Test Plugin Display', got %q", m.Interface.DisplayName)
	}
	if m.Interface.Category != "Productivity" {
		t.Errorf("expected category 'Productivity', got %q", m.Interface.Category)
	}
	if len(m.Interface.Capabilities) != 2 {
		t.Errorf("expected 2 capabilities, got %d", len(m.Interface.Capabilities))
	}
}

func TestParseManifest_InvalidJSON(t *testing.T) {
	data := []byte(`{ "name": "test", invalid json }`)

	m, err := ParseManifest(data)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
	if m != nil {
		t.Errorf("expected nil manifest for invalid JSON, got %v", m)
	}
}

func TestParseManifest_EmptyInput(t *testing.T) {
	data := []byte{}

	m, err := ParseManifest(data)
	if err != nil {
		t.Fatalf("expected no error for empty input, got %v", err)
	}
	if m != nil {
		t.Errorf("expected nil manifest for empty input, got %v", m)
	}
}

func TestParseManifest_MissingName(t *testing.T) {
	data := []byte(`{ "version": "1.0.0" }`)

	m, err := ParseManifest(data)
	if err == nil {
		t.Fatal("expected error for missing name, got nil")
	}
	if m != nil {
		t.Errorf("expected nil manifest for missing name, got %v", m)
	}
}
