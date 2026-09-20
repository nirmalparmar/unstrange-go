package db

import (
	"context"
	"log"
)

// Migrate runs initial schema migrations.
// In production, use a proper migration tool (goose, atlas, etc.)
func Migrate() {
	queries := []string{
		`CREATE EXTENSION IF NOT EXISTS "uuid-ossp"`,

		// ── Users (existing) ──
		`CREATE TABLE IF NOT EXISTS users (
			id            UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
			email         TEXT UNIQUE,
			display_name  TEXT,
			avatar_url    TEXT,
			provider      TEXT NOT NULL,
			provider_id   TEXT NOT NULL,
			is_verified   BOOLEAN DEFAULT FALSE,
			is_active     BOOLEAN DEFAULT TRUE,
			created_at    TIMESTAMPTZ DEFAULT NOW(),
			updated_at    TIMESTAMPTZ DEFAULT NOW(),
			UNIQUE (provider, provider_id)
		)`,

		// ── Refresh tokens (existing) ──
		`CREATE TABLE IF NOT EXISTS refresh_tokens (
			id         UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
			user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			token      TEXT NOT NULL UNIQUE,
			expires_at TIMESTAMPTZ NOT NULL,
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`,

		// ── Phone OTPs (short-lived, cleaned up after verification) ──
		`CREATE TABLE IF NOT EXISTS phone_otps (
			id         UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
			phone      TEXT NOT NULL,
			otp        TEXT NOT NULL,
			req_id     TEXT DEFAULT '',
			attempts   INT DEFAULT 0,
			expires_at TIMESTAMPTZ NOT NULL,
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_phone_otps_phone ON phone_otps(phone)`,
		`CREATE INDEX IF NOT EXISTS idx_phone_otps_expires ON phone_otps(expires_at)`,

		// ── Profiles (extends users with social info) ──
		`CREATE TABLE IF NOT EXISTS profiles (
			user_id       UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
			username      TEXT UNIQUE NOT NULL,
			bio           TEXT DEFAULT '',
			location      TEXT DEFAULT '',
			website       TEXT DEFAULT '',
			date_of_birth DATE,
			gender        TEXT DEFAULT '',
			instagram     TEXT DEFAULT '',
			interests     TEXT[] DEFAULT '{}',
			is_onboarded  BOOLEAN DEFAULT FALSE,
			is_private    BOOLEAN DEFAULT FALSE,
			created_at    TIMESTAMPTZ DEFAULT NOW(),
			updated_at    TIMESTAMPTZ DEFAULT NOW()
		)`,
		// Add is_private column if it doesn't exist (for existing DBs)
		`DO $$ BEGIN
			ALTER TABLE profiles ADD COLUMN IF NOT EXISTS is_private BOOLEAN DEFAULT FALSE;
		EXCEPTION WHEN others THEN NULL;
		END $$`,

		// ── Posts (posts + reels share the same table) ──
		`CREATE TABLE IF NOT EXISTS posts (
			id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
			user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			caption     TEXT DEFAULT '',
			location    TEXT DEFAULT '',
			type        TEXT NOT NULL DEFAULT 'post',
			created_at  TIMESTAMPTZ DEFAULT NOW(),
			updated_at  TIMESTAMPTZ DEFAULT NOW()
		)`,

		// ── Post media (multiple media per post) ──
		`CREATE TABLE IF NOT EXISTS post_media (
			id         UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
			post_id    UUID NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
			url        TEXT NOT NULL,
			type       TEXT NOT NULL DEFAULT 'image',
			width      INT DEFAULT 0,
			height     INT DEFAULT 0,
			duration   FLOAT DEFAULT 0,
			sort_order INT DEFAULT 0,
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`,

		// ── Stories (24h ephemeral content) ──
		`CREATE TABLE IF NOT EXISTS stories (
			id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
			user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			media_url   TEXT NOT NULL,
			media_type  TEXT NOT NULL DEFAULT 'image',
			duration    FLOAT DEFAULT 5,
			expires_at  TIMESTAMPTZ NOT NULL,
			created_at  TIMESTAMPTZ DEFAULT NOW()
		)`,

		// ── Story views ──
		`CREATE TABLE IF NOT EXISTS story_views (
			story_id   UUID NOT NULL REFERENCES stories(id) ON DELETE CASCADE,
			viewer_id  UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			viewed_at  TIMESTAMPTZ DEFAULT NOW(),
			PRIMARY KEY (story_id, viewer_id)
		)`,

		// ── Likes ──
		`CREATE TABLE IF NOT EXISTS likes (
			id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
			user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			post_id     UUID NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
			created_at  TIMESTAMPTZ DEFAULT NOW(),
			UNIQUE (user_id, post_id)
		)`,

		// ── Comments (with threading via parent_id) ──
		`CREATE TABLE IF NOT EXISTS comments (
			id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
			user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			post_id     UUID NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
			parent_id   UUID REFERENCES comments(id) ON DELETE CASCADE,
			body        TEXT NOT NULL,
			created_at  TIMESTAMPTZ DEFAULT NOW(),
			updated_at  TIMESTAMPTZ DEFAULT NOW()
		)`,

		// ── Follows ──
		`CREATE TABLE IF NOT EXISTS follows (
			follower_id  UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			following_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			created_at   TIMESTAMPTZ DEFAULT NOW(),
			PRIMARY KEY (follower_id, following_id),
			CHECK (follower_id != following_id)
		)`,

		// ── Bookmarks ──
		`CREATE TABLE IF NOT EXISTS bookmarks (
			id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
			user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			post_id     UUID NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
			created_at  TIMESTAMPTZ DEFAULT NOW(),
			UNIQUE (user_id, post_id)
		)`,

		// ── Activities ──
		`CREATE TABLE IF NOT EXISTS activities (
			id            UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
			creator_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			title         TEXT NOT NULL,
			description   TEXT DEFAULT '',
			location      TEXT DEFAULT '',
			cover_url     TEXT DEFAULT '',
			max_participants INT DEFAULT 0,
			starts_at     TIMESTAMPTZ,
			ends_at       TIMESTAMPTZ,
			is_active     BOOLEAN DEFAULT TRUE,
			created_at    TIMESTAMPTZ DEFAULT NOW(),
			updated_at    TIMESTAMPTZ DEFAULT NOW()
		)`,

		// ── Activity participants ──
		`CREATE TABLE IF NOT EXISTS activity_participants (
			activity_id UUID NOT NULL REFERENCES activities(id) ON DELETE CASCADE,
			user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			status      TEXT NOT NULL DEFAULT 'joined',
			joined_at   TIMESTAMPTZ DEFAULT NOW(),
			PRIMARY KEY (activity_id, user_id)
		)`,

		// ── Indexes ──
		`CREATE INDEX IF NOT EXISTS idx_users_email ON users(email)`,
		`CREATE INDEX IF NOT EXISTS idx_users_provider ON users(provider, provider_id)`,
		`CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user ON refresh_tokens(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_profiles_username ON profiles(username)`,
		`CREATE INDEX IF NOT EXISTS idx_posts_user ON posts(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_posts_type ON posts(type)`,
		`CREATE INDEX IF NOT EXISTS idx_posts_created ON posts(created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_post_media_post ON post_media(post_id)`,
		`CREATE INDEX IF NOT EXISTS idx_stories_user ON stories(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_stories_expires ON stories(expires_at)`,
		`CREATE INDEX IF NOT EXISTS idx_likes_post ON likes(post_id)`,
		`CREATE INDEX IF NOT EXISTS idx_likes_user_post ON likes(user_id, post_id)`,
		`CREATE INDEX IF NOT EXISTS idx_comments_post ON comments(post_id)`,
		`CREATE INDEX IF NOT EXISTS idx_follows_follower ON follows(follower_id)`,
		`CREATE INDEX IF NOT EXISTS idx_follows_following ON follows(following_id)`,
		`CREATE INDEX IF NOT EXISTS idx_bookmarks_user ON bookmarks(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_activities_creator ON activities(creator_id)`,
		`CREATE INDEX IF NOT EXISTS idx_activities_starts ON activities(starts_at)`,
		`CREATE INDEX IF NOT EXISTS idx_activity_participants_user ON activity_participants(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_activity_participants_activity ON activity_participants(activity_id)`,
	}

	for _, q := range queries {
		if _, err := Pool.Exec(context.Background(), q); err != nil {
			log.Fatalf("migration failed: %v\nquery: %s", err, q)
		}
	}

	log.Println("migrations complete")
}
