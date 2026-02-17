package chatbot

/**
 * @Author linya.jj
 * @Date 2023/3/22 18:30
 */

type BotCallbackDataAtUserModel struct {
	DingtalkId string `json:"dingtalkId"`
	StaffId    string `json:"staffId"`
}

type BotCallbackDataTextModel struct {
	Content string `json:"content"`
}

type BotCallbackDataModel struct {
	ConversationId            string                       `json:"conversationId"`
	AtUsers                   []BotCallbackDataAtUserModel `json:"atUsers"`
	ChatbotCorpId             string                       `json:"chatbotCorpId"`
	ChatbotUserId             string                       `json:"chatbotUserId"`
	MsgId                     string                       `json:"msgId"`
	SenderNick                string                       `json:"senderNick"`
	IsAdmin                   bool                         `json:"isAdmin"`
	SenderStaffId             string                       `json:"senderStaffId"`
	SessionWebhookExpiredTime int64                        `json:"sessionWebhookExpiredTime"`
	CreateAt                  int64                        `json:"createAt"`
	SenderCorpId              string                       `json:"senderCorpId"`
	ConversationType          string                       `json:"conversationType"`
	SenderId                  string                       `json:"senderId"`
	ConversationTitle         string                       `json:"conversationTitle"`
	IsInAtList                bool                         `json:"isInAtList"`
	SessionWebhook            string                       `json:"sessionWebhook"`
	Text                      BotCallbackDataTextModel     `json:"text"`
	Msgtype                   string                       `json:"msgtype"`
	Content                   interface{}                  `json:"content"`
}
