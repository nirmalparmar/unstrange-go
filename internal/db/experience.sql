-- Interactive screen features. All additions are safe to apply on existing data.
ALTER TABLE posts ADD COLUMN IF NOT EXISTS audience TEXT NOT NULL DEFAULT 'public' CHECK(audience IN ('public','followers'));
CREATE TABLE IF NOT EXISTS private_media (
 id UUID PRIMARY KEY, owner_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 purpose TEXT NOT NULL CHECK(purpose IN ('verification','chat')),
 conversation_id UUID REFERENCES conversations(id) ON DELETE CASCADE,
 ciphertext BYTEA NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), expires_at TIMESTAMPTZ,
 CHECK((purpose='chat')=(conversation_id IS NOT NULL))
);
CREATE INDEX IF NOT EXISTS private_media_expiry ON private_media(expires_at);
ALTER TABLE verification_requests ADD COLUMN IF NOT EXISTS document_id UUID REFERENCES private_media(id) ON DELETE SET NULL;
ALTER TABLE verification_requests ADD COLUMN IF NOT EXISTS selfie_id UUID REFERENCES private_media(id) ON DELETE SET NULL;
ALTER TABLE verification_requests ADD COLUMN IF NOT EXISTS document_type TEXT NOT NULL DEFAULT '';
ALTER TABLE verification_requests ADD COLUMN IF NOT EXISTS consent_at TIMESTAMPTZ;
ALTER TABLE messages ADD COLUMN IF NOT EXISTS attachment_id UUID REFERENCES private_media(id) ON DELETE SET NULL;
ALTER TABLE messages ADD COLUMN IF NOT EXISTS plan_id UUID REFERENCES activities(id) ON DELETE SET NULL;
ALTER TABLE messages ADD COLUMN IF NOT EXISTS post_id UUID REFERENCES posts(id) ON DELETE SET NULL;
ALTER TABLE messages ADD COLUMN IF NOT EXISTS story_id UUID REFERENCES stories(id) ON DELETE SET NULL;
CREATE TABLE IF NOT EXISTS conversation_reads (
 conversation_id UUID REFERENCES conversations(id) ON DELETE CASCADE,
 user_id UUID REFERENCES users(id) ON DELETE CASCADE, last_message_id BIGINT NOT NULL DEFAULT 0,
 PRIMARY KEY(conversation_id,user_id)
);
ALTER TABLE notifications ADD COLUMN IF NOT EXISTS actor_id UUID REFERENCES users(id) ON DELETE CASCADE;
ALTER TABLE notifications ADD COLUMN IF NOT EXISTS post_id UUID REFERENCES posts(id) ON DELETE CASCADE;
ALTER TABLE notifications ADD COLUMN IF NOT EXISTS comment_id UUID REFERENCES comments(id) ON DELETE CASCADE;
ALTER TABLE notifications ADD COLUMN IF NOT EXISTS kind TEXT NOT NULL DEFAULT 'update';
CREATE TABLE IF NOT EXISTS comment_likes (
 comment_id UUID REFERENCES comments(id) ON DELETE CASCADE, user_id UUID REFERENCES users(id) ON DELETE CASCADE,
 PRIMARY KEY(comment_id,user_id)
);
CREATE TABLE IF NOT EXISTS follow_requests (
 requester_id UUID REFERENCES users(id) ON DELETE CASCADE, recipient_id UUID REFERENCES users(id) ON DELETE CASCADE,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), PRIMARY KEY(requester_id,recipient_id), CHECK(requester_id!=recipient_id)
);
ALTER TABLE event_questions ADD COLUMN IF NOT EXISTS answered_at TIMESTAMPTZ;
ALTER TABLE reports ADD COLUMN IF NOT EXISTS conversation_id UUID REFERENCES conversations(id) ON DELETE SET NULL;
ALTER TABLE reports ADD COLUMN IF NOT EXISTS post_id UUID REFERENCES posts(id) ON DELETE SET NULL;
-- Social notifications are created in the same transaction as the interaction.
CREATE OR REPLACE FUNCTION notify_social_interaction() RETURNS trigger AS $$
DECLARE target UUID; post_ref UUID; parent_ref UUID; event_kind TEXT; event_title TEXT;
BEGIN
 IF TG_TABLE_NAME='likes' THEN
  SELECT user_id INTO target FROM posts WHERE id=NEW.post_id;
  post_ref:=NEW.post_id; event_kind:='like'; event_title:='Liked your moment';
 ELSIF TG_TABLE_NAME='follows' THEN
  target:=NEW.following_id; event_kind:='follow'; event_title:='Started following you';
  IF target!=NEW.follower_id THEN INSERT INTO notifications(user_id,actor_id,title,kind) VALUES(target,NEW.follower_id,event_title,event_kind); END IF;
  RETURN NEW;
 ELSE
  post_ref:=NEW.post_id; parent_ref:=NEW.id; event_kind:='comment'; event_title:='Commented on your moment';
  IF NEW.parent_id IS NOT NULL THEN
   SELECT user_id INTO target FROM comments WHERE id=NEW.parent_id;
   event_kind:='reply'; event_title:='Replied to your comment';
  ELSE SELECT user_id INTO target FROM posts WHERE id=NEW.post_id; END IF;
 END IF;
 IF target!=NEW.user_id THEN
  INSERT INTO notifications(user_id,actor_id,post_id,comment_id,title,body,kind)
  VALUES(target,NEW.user_id,post_ref,parent_ref,event_title,CASE WHEN TG_TABLE_NAME='comments' THEN to_jsonb(NEW)->>'body' ELSE '' END,event_kind);
 END IF;
 RETURN NEW;
END; $$ LANGUAGE plpgsql;
DROP TRIGGER IF EXISTS likes_notify ON likes;
CREATE TRIGGER likes_notify AFTER INSERT ON likes FOR EACH ROW EXECUTE FUNCTION notify_social_interaction();
DROP TRIGGER IF EXISTS follows_notify ON follows;
CREATE TRIGGER follows_notify AFTER INSERT ON follows FOR EACH ROW EXECUTE FUNCTION notify_social_interaction();
DROP TRIGGER IF EXISTS comments_notify ON comments;
CREATE TRIGGER comments_notify AFTER INSERT ON comments FOR EACH ROW EXECUTE FUNCTION notify_social_interaction();
