package storage

import "testing"

func TestSetManyWritesMultipleBuckets(t *testing.T) {
	db, err := Open(t.TempDir() + "/bot.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := db.SetMany(
		Entry{Bucket: "global", Key: "project", Value: 19},
		Entry{Bucket: "channel", Key: "primary\x00#chat\x00project", Value: 9},
	); err != nil {
		t.Fatalf("SetMany returned error: %v", err)
	}
	global, err := db.Get("global", "project")
	if err != nil || string(global) != "19" {
		t.Fatalf("global value = %q, %v; want 19", global, err)
	}
	channel, err := db.Get("channel", "primary\x00#chat\x00project")
	if err != nil || string(channel) != "9" {
		t.Fatalf("channel value = %q, %v; want 9", channel, err)
	}
}

func TestSetManyRejectsIncompleteEntryWithoutWriting(t *testing.T) {
	db, err := Open(t.TempDir() + "/bot.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := db.SetMany(Entry{Bucket: "global", Key: "project", Value: 19}, Entry{Bucket: "", Key: "bad", Value: 1}); err == nil {
		t.Fatal("SetMany unexpectedly accepted an incomplete entry")
	}
	if _, err := db.Get("global", "project"); err != ErrNotFound {
		t.Fatalf("partial write found: %v", err)
	}
}
