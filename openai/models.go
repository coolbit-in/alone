package openai

import "gorm.io/gorm"

type Conversation struct {
	gorm.Model
	Name         string                  `json:"name"`
	SystemRoleID uint                    `json:"system_role_id,omitempty"`
	Messages     []ChatCompletionMessage `json:"messages,omitempty"`
}

// conversation table name conversation
func (Conversation) TableName() string {
	return "conversation"
}

type ChatCompletionMessage struct {
	gorm.Model
	ConversationID   uint   `gorm:"index" json:"conversation_id"`
	Role             string `json:"role"`
	Content          string `json:"content"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
}

func (ChatCompletionMessage) TableName() string {
	return "chat_completion_message"
}

type SystemRole struct {
	gorm.Model
	Name    string `gorm:"index" json:"name,omitempty"`
	Content string `json:"content,omitempty"`
}

func (SystemRole) TableName() string {
	return "system_role"
}
