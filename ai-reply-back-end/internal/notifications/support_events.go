package notifications

import (
	"context"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
)

// LinkSupport — қолдау чатының экраны (ескі build-тер Home ашады).
const LinkSupport = "aireply://support"

// SupportReplyTTL — оператор жауабының push-ы осыдан кейін жіберілмейді (кезекте қалып қойса).
//
// After a day the chat's unread badge tells the same, and a late "support
// replied" about an answer already read would only confuse.
const SupportReplyTTL = 24 * time.Hour

// SupportReplied — оператор қолдау чатында жауап берді (support.Notifier).
//
// One push per unread burst: the key holds the oldest operator message the
// person has not read yet, so further replies before they open the chat find
// the same notification, and the first reply after they read it starts a new
// one. Category account; the text never contains the reply itself. It answers
// the person's own question: transactional, sent at once even inside the
// reminder quiet hours (no Defer), and worth SupportReplyTTL in the queue.
func (e *Events) SupportReplied(ctx context.Context, userID, conversationID, firstUnreadMessageID string) {
	if userID == "" || conversationID == "" || firstUnreadMessageID == "" {
		return
	}
	ctx, cancel := detached(ctx, eventTimeout)
	defer cancel()
	e.notify(ctx, UserNotification{
		UserID:         userID,
		IdempotencyKey: domain.TypeSupportReply + ":" + conversationID + ":" + firstUnreadMessageID,
		Type:           domain.TypeSupportReply,
		Category:       domain.CategoryAccount,
		Link:           LinkSupport,
		Data:           map[string]string{"conversation_id": conversationID},
		TitleKey:       "push.support_reply.title",
		BodyKey:        "push.support_reply.body",
		Transactional:  true,
	})
}
