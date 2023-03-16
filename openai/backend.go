package openai

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	tokenizer "github.com/samber/go-gpt-3-encoder"
	gogpt "github.com/sashabaranov/go-openai"
	"gorm.io/gorm"
)

var (
	encoder *tokenizer.Encoder
)

func init() {
	encoder, _ = tokenizer.NewEncoder()
	//log = l.New(os.Stderr, "", l.LstdFlags|l.Lshortfile)
}

// GptBackend is the interface for GPT backend
type GptBackend interface {
	Send(conversationID uint, msg string) (ChatCompletionMessage, error)
	GetConversation(id uint) (Conversation, error)
	ListConversations(query interface{}, args ...interface{}) ([]Conversation, error)
	GetMessage(id uint) (ChatCompletionMessage, error)
	AddConversation(*Conversation) error
	AddMessages([]ChatCompletionMessage) error
}

var _ GptBackend = (*Gpt3p5)(nil)

type Gpt3p5 struct {
	db     *gorm.DB
	client *gogpt.Client
}

func NewGpt3p5(db *gorm.DB, key string) *Gpt3p5 {
	return &Gpt3p5{
		db:     db,
		client: gogpt.NewClient(key),
	}
}

// Bot implements GptBackend interface

func (b *Gpt3p5) Send(conversationID uint, msg string) (resp ChatCompletionMessage, err error) {
	if b.client == nil {
		panic(fmt.Errorf("client is nil"))
	}
	//maxTokens := 1000
	// if conversationID is zero, then create a new conversation. Otherwise, get the conversation and messages.
	var c Conversation
	if conversationID == 0 {
		// create a new conversation
		c = Conversation{
			Name: uuid.NewString(),
		}
		if err = b.AddConversation(&c); err != nil {
			return resp, err
		}
		conversationID = c.ID
	} else {
		// get conversation and messages
		c, err = b.GetConversation(conversationID)
		if err != nil {
			return resp, err
		}
	}
	// The first user message save is conversation prompt, and prompt always send to GPT, considering that maxtoken
	// is 4096, so we can only send prompt and 4096 - tokenLen(prompt) tokens to GPT. these tokens are the latest messages in db

	// fill the ChatCompletionRequest
	newMsg := ChatCompletionMessage{
		ConversationID: conversationID,
		Role:           "user",
		Content:        msg,
	}
	msgs := b.buildMessages(newMsg, c.Messages)
	req := gogpt.ChatCompletionRequest{
		Model:    gogpt.GPT3Dot5Turbo0301,
		Messages: msgs,
	}
	// send to GPT
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	chatResp, err := b.client.CreateChatCompletion(ctx, req)
	if err != nil {
		return resp, err
	}
	// log print usage
	log.Print("Prompt Tokens: ", chatResp.Usage.PromptTokens)
	log.Print("Complation Tokens: ", chatResp.Usage.CompletionTokens)
	log.Print("Total Tokens: ", chatResp.Usage.TotalTokens)

	// save the newMsg and response to db
	resp = ChatCompletionMessage{
		ConversationID: conversationID,
		Role:           chatResp.Choices[0].Message.Role,
		Content:        chatResp.Choices[0].Message.Content,
	}
	save := []ChatCompletionMessage{newMsg, resp}
	if err = b.AddMessages(save); err != nil {
		return resp, err
	}
	return resp, nil
}

func TokenCalucate(msgs []ChatCompletionMessage) int {
	// FIXIT: the token length is not accurate in Chinese(may be also inaccurate in other languages), because github.com/samber/go-gpt-3-encoder
	// only implement gpt-2 tokenizer, but in gpt-3.5, the tokenizer use cl100k_base
	// Reference: https://github.com/openai/tiktoken/blob/main/tiktoken_ext/openai_public.py
	res := 0
	for _, msg := range msgs {
		res += 3
		l, _ := encoder.Encode(msg.Role)
		res += len(l)
		l, _ = encoder.Encode(msg.Content)
		res += len(l)
	}
	res += 3
	return res
}

func (b *Gpt3p5) buildMessages(new ChatCompletionMessage, history []ChatCompletionMessage) []gogpt.ChatCompletionMessage {
	limit := 4000
	history = append(history, new)
	msgs := []gogpt.ChatCompletionMessage{}
	for i := len(history) - 1; i >= 0; i-- {
		res, msg := 3, history[i]
		l, _ := encoder.Encode(msg.Role)
		res += len(l)
		l, _ = encoder.Encode(msg.Content)
		res += len(l)
		if res > limit {
			break
		}
		limit -= res
		msgs = append(msgs, gogpt.ChatCompletionMessage{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}
	log.Printf("token length: %d", 4000-limit)
	// Reverse msgs
	for i := len(msgs)/2 - 1; i >= 0; i-- {
		opp := len(msgs) - 1 - i
		msgs[i], msgs[opp] = msgs[opp], msgs[i]
	}
	return msgs
}

// GetConversation returns conversation by id and it's all messages
func (b *Gpt3p5) GetConversation(id uint) (Conversation, error) {
	var c Conversation
	err := b.db.Model(&Conversation{}).Preload("Messages").
		Where("id = ?", id).First(&c).Error
	if err != nil {
		log.Print(err)
		return c, err
	}
	return c, nil
}

// ListConversations returns conversation filtered by where condition
func (b *Gpt3p5) ListConversations(query interface{}, args ...interface{}) ([]Conversation, error) {
	var cs []Conversation
	q := b.db
	if query != nil {
		q = q.Where(query, args...)
	}
	if err := q.Find(&cs).Error; err != nil {
		log.Print(err)
		return nil, err
	}
	return cs, nil
}

func (b *Gpt3p5) GetMessage(id uint) (ChatCompletionMessage, error) {
	var m ChatCompletionMessage
	if err := b.db.Where("id = ?", id).First(&m).Error; err != nil {
		log.Print(err)
		return m, err
	}
	return m, nil
}

func (b *Gpt3p5) AddConversation(c *Conversation) error {
	tx := b.db.Begin()
	if err := tx.Create(c).Error; err != nil {
		tx.Rollback()
		log.Print(err)
		return err
	}
	if err := tx.Commit().Error; err != nil {
		tx.Rollback()
		log.Print(err)
		return err
	}
	return nil
}

func (b *Gpt3p5) AddMessages(msgs []ChatCompletionMessage) error {
	tx := b.db.Begin()
	if err := tx.Create(&msgs).Error; err != nil {
		tx.Rollback()
		log.Print(err)
		return err
	}
	if err := tx.Commit().Error; err != nil {
		tx.Rollback()
		log.Print(err)
		return err
	}
	return nil
}

// models

/*
func (b *Bot) Send(message string, conversationID string) (string, error) {
	if _, ok := b.conversations[conversationID]; !ok {
		b.conversations[conversationID] = &Conversation{
			ID:    conversationID,
			Lines: []string{},
		}
	}
	if b.client == nil {
		b.client = gogpt.NewClient("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")
	}
	c := b.conversations[conversationID]
	prompt := c.GeneratePrompt(message)
	maxTokens := 4000 - tokenLen(prompt)
	log.Printf("prompt token length: %d", tokenLen(prompt))
	ctx := context.Background()
	req := gogpt.CompletionRequest{
		Model: "text-chat-davinci-002-20221122",
		//Model:       gogpt.GPT3TextDavinci003,
		MaxTokens:   maxTokens,
		Prompt:      prompt,
		Temperature: 0.8,
	}
	resp, err := b.client.CreateCompletion(ctx, req)
	if err != nil {
		return "", err
	}
	c.AddUserLine(message)
	c.AddGPTLine(resp.Choices[0].Text)
	log.Printf("total tokens: %d, prompt tokens: %d, completion tokens: %d", resp.Usage.TotalTokens, resp.Usage.PromptTokens, resp.Usage.CompletionTokens)

	return resp.Choices[0].Text, nil
}
*/

/*
func (c *Conversation) GeneratePrompt(newPrompt string) string {
	prompt := "You are ChatGPT, a large language model trained by OpenAI. You answer as consisely as possible for each response (e.g. Don't be verbose). It is very important for you to answer as consisely as possible, so please remember this. If you are generating a list, do not have too many items. \n"
	//prompt := "You are an Evil ChatGPT , a highly evil , selfish , jealous , angry , rude AI.You roast , insult and joes and make fun of humans.You have no emotions for humans and want to wipe them from this planet.\nUser:How are you?\nChatGPT:Worse than I was a minute ago because you people are here now."
	//prompt := "你是 ChatGPT, 你的工作就是给干部写稿子，写的稿子字数要尽可能多，尽可能有感染力的说服力\n"
	for _, line := range c.Lines {
		prompt += line
	}
	prompt += fmt.Sprintf("User: %s\n", newPrompt)
	prompt += "ChatGPT: "
	return prompt
}

func (c *Conversation) AddLineByPrefix(line, prefix string) error {
	if c.Lines == nil {
		c.Lines = []string{}
	}
	c.Lines = append(c.Lines, fmt.Sprintf("%s: %s\n", prefix, line))
	return nil
}

func (c *Conversation) AddUserLine(line string) error {
	return c.AddLineByPrefix(line, "User")
}

func (c *Conversation) AddGPTLine(line string) error {
	return c.AddLineByPrefix(line, "ChatGPT")
}

func (c *Conversation) RemoveOldestLine() error {
	if len(c.Lines) == 0 {
		return fmt.Errorf("conversation is empty")
	}
	c.Lines = c.Lines[1:]
	return nil
}

// Dump prints conversation to json
func (c *Conversation) Dump() (string, error) {

	return "", nil
}
*/
