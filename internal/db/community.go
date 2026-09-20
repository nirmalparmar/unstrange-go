package db

import (
	"context"
	_ "embed"
	"log"
)

//go:embed community.sql
var communitySchema string

//go:embed experience.sql
var experienceSchema string

func MigrateCommunity() {
	if _, err := Pool.Exec(context.Background(), communitySchema+"\n"+experienceSchema); err != nil {
		log.Fatalf("community migration: %v", err)
	}
}
