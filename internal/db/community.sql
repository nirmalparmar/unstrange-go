ALTER TABLE profiles ADD COLUMN IF NOT EXISTS latitude DOUBLE PRECISION;
ALTER TABLE profiles ADD COLUMN IF NOT EXISTS longitude DOUBLE PRECISION;
ALTER TABLE profiles ADD COLUMN IF NOT EXISTS discoverable BOOLEAN NOT NULL DEFAULT FALSE;
CREATE TABLE IF NOT EXISTS blocks (
 blocker_id UUID REFERENCES users(id) ON DELETE CASCADE,
 blocked_id UUID REFERENCES users(id) ON DELETE CASCADE,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 PRIMARY KEY(blocker_id,blocked_id), CHECK(blocker_id != blocked_id)
);
CREATE TABLE IF NOT EXISTS events (
 id UUID PRIMARY KEY DEFAULT uuid_generate_v4(), owner_id UUID NOT NULL REFERENCES users(id),
 title TEXT NOT NULL, description TEXT NOT NULL DEFAULT '', organization TEXT NOT NULL DEFAULT '',
 cover_url TEXT NOT NULL DEFAULT '', location TEXT NOT NULL DEFAULT '',
 starts_at TIMESTAMPTZ NOT NULL, ends_at TIMESTAMPTZ NOT NULL,
 invite_code UUID NOT NULL UNIQUE DEFAULT uuid_generate_v4(),
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), CHECK(ends_at > starts_at)
);
CREATE TABLE IF NOT EXISTS event_members (
 event_id UUID REFERENCES events(id) ON DELETE CASCADE, user_id UUID REFERENCES users(id) ON DELETE CASCADE,
 joined_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), PRIMARY KEY(event_id,user_id)
);
ALTER TABLE activities ADD COLUMN IF NOT EXISTS category TEXT NOT NULL DEFAULT 'Social';
ALTER TABLE activities ADD COLUMN IF NOT EXISTS visibility TEXT NOT NULL DEFAULT 'public';
ALTER TABLE activities ADD COLUMN IF NOT EXISTS approval TEXT NOT NULL DEFAULT 'instant';
ALTER TABLE activities ADD COLUMN IF NOT EXISTS latitude DOUBLE PRECISION;
ALTER TABLE activities ADD COLUMN IF NOT EXISTS longitude DOUBLE PRECISION;
ALTER TABLE activities ADD COLUMN IF NOT EXISTS event_id UUID REFERENCES events(id) ON DELETE CASCADE;
CREATE TABLE IF NOT EXISTS conversations (
 id UUID PRIMARY KEY DEFAULT uuid_generate_v4(), requester_id UUID REFERENCES users(id), recipient_id UUID REFERENCES users(id),
 activity_id UUID UNIQUE REFERENCES activities(id) ON DELETE CASCADE,
 event_id UUID UNIQUE REFERENCES events(id) ON DELETE CASCADE,
 state TEXT NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','accepted','declined')),
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 CHECK(num_nonnulls(activity_id,event_id,recipient_id)=1), CHECK(requester_id != recipient_id)
);
CREATE UNIQUE INDEX IF NOT EXISTS conversations_pair ON conversations (LEAST(requester_id,recipient_id),GREATEST(requester_id,recipient_id)) WHERE recipient_id IS NOT NULL;
CREATE TABLE IF NOT EXISTS messages (
 id BIGSERIAL PRIMARY KEY, conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
 sender_id UUID NOT NULL REFERENCES users(id), body TEXT NOT NULL CHECK(length(body) BETWEEN 1 AND 4000),
 client_id UUID NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), UNIQUE(sender_id,client_id)
);
CREATE INDEX IF NOT EXISTS messages_conversation ON messages(conversation_id,id);
CREATE TABLE IF NOT EXISTS notifications (
 id UUID PRIMARY KEY DEFAULT uuid_generate_v4(), user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 title TEXT NOT NULL, body TEXT NOT NULL DEFAULT '', activity_id UUID REFERENCES activities(id) ON DELETE CASCADE,
 conversation_id UUID REFERENCES conversations(id) ON DELETE CASCADE, read_at TIMESTAMPTZ, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS notifications_user ON notifications(user_id,created_at DESC);
CREATE TABLE IF NOT EXISTS reports (
 id UUID PRIMARY KEY DEFAULT uuid_generate_v4(), reporter_id UUID NOT NULL REFERENCES users(id),
 target_user_id UUID NOT NULL REFERENCES users(id), reason TEXT NOT NULL, details TEXT NOT NULL DEFAULT '',
 status TEXT NOT NULL DEFAULT 'open', created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE IF NOT EXISTS meetup_reviews (
 activity_id UUID REFERENCES activities(id) ON DELETE CASCADE, reviewer_id UUID REFERENCES users(id),
 target_user_id UUID REFERENCES users(id), rating INT NOT NULL CHECK(rating BETWEEN 1 AND 5),
 body TEXT NOT NULL DEFAULT '', created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 PRIMARY KEY(activity_id,reviewer_id,target_user_id), CHECK(reviewer_id != target_user_id)
);
CREATE TABLE IF NOT EXISTS event_questions (
 id UUID PRIMARY KEY DEFAULT uuid_generate_v4(), event_id UUID NOT NULL REFERENCES events(id) ON DELETE CASCADE,
 user_id UUID NOT NULL REFERENCES users(id), body TEXT NOT NULL, answer TEXT NOT NULL DEFAULT '', created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS activities_discover ON activities(is_active,starts_at,category);
CREATE TABLE IF NOT EXISTS request_limits (
 scope TEXT NOT NULL, subject TEXT NOT NULL, bucket TIMESTAMPTZ NOT NULL,
 count INT NOT NULL DEFAULT 1, PRIMARY KEY(scope,subject,bucket)
);
CREATE TABLE IF NOT EXISTS verification_requests (
 user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','verified','declined')),
 reviewed_by UUID REFERENCES users(id), note TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), reviewed_at TIMESTAMPTZ
);
CREATE TABLE IF NOT EXISTS moderation_audit (
 id BIGSERIAL PRIMARY KEY, reviewer_id UUID NOT NULL REFERENCES users(id),
 report_id UUID NOT NULL REFERENCES reports(id), action TEXT NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Preserve existing sessions while replacing stored bearer tokens with digests.
UPDATE refresh_tokens
SET token = 'sha256:' || encode(sha256(convert_to(token, 'UTF8')), 'hex')
WHERE token NOT LIKE 'sha256:%';
