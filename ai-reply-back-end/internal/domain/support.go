package domain

import "time"

// Қолдау чатының күйлері.
const (
	SupportOpen     = "open"
	SupportResolved = "resolved"
)

// SupportStatuses — сөйлесу күйлері (әкімші сүзгісі үшін).
var SupportStatuses = []string{SupportOpen, SupportResolved}

// Хабарламаны кім жазды. Оператордың аты мен поштасы қолданушыға ешқашан көрсетілмейді.
const (
	SupportSenderUser     = "user"
	SupportSenderOperator = "operator"
)

// SupportTextMax — бір хабарламаның ең көп таңба саны.
const SupportTextMax = 2000

// SupportConversation — бір тіркелгінің қолдау қызметімен жалғыз сөйлесуі.
type SupportConversation struct {
	ID              string
	UserID          string
	Status          string
	AssignedAdminID string
	LastMessageAt   time.Time
	// UserUnread — the operator messages the person has not read yet.
	UserUnread int
	// OperatorUnread — the person's messages no operator has read yet.
	OperatorUnread int
	ResolvedAt     *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// SupportMessage — сөйлесудегі бір хабарлама.
type SupportMessage struct {
	ID              string
	ConversationID  string
	Sender          string
	AdminID         string
	ClientMessageID string
	Body            string
	CreatedAt       time.Time
	ReadAt          *time.Time
}
