package notifications

import (
	"context"

	"github.com/aireply/ai-reply-back-end/internal/domain"
)

// LinkSupport — қолдау чатының экраны (ескі build-тер Home ашады).
const LinkSupport = "aireply://support"

// SupportReplied — оператор қолдау чатында жауап берді (support.Notifier).
//
// One push per unread burst: the key holds the oldest operator message the
// person has not read yet, so further replies before they open the chat find
// the same notification, and the first reply after they read it starts a new
// one. Category account; the text never contains the reply itself.
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
	})
}
