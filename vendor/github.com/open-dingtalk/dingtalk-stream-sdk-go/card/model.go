package card

type PrivateCardActionData struct {
	CardPrivateData CardPrivateData `json:"cardPrivateData"`
}

type CardPrivateData struct {
	ActionIdList []string       `json:"actionIds"`
	Params       map[string]any `json:"params"`
}

type CardUpdateOptions struct {
	UpdateCardDataByKey    bool `json:"updateCardDataByKey"`
	UpdatePrivateDataByKey bool `json:"updatePrivateDataByKey"`
}

type CardDataDto struct {
	CardParamMap map[string]string `json:"cardParamMap"`
}

type CardRequest struct {
	Content        string `json:"content"`
	CorpId         string `json:"corpId"`
	Extension      string `json:"extension"`
	OutTrackId     string `json:"outTrackId"`
	SpaceId        string `json:"spaceId"`
	SpaceType      string `json:"spaceType"`
	Type           string `json:"type"`
	UserId         string `json:"userId"`
	UserIdType     int    `json:"userIdType"`
	CardActionData PrivateCardActionData
}

type CardResponse struct {
	CardUpdateOptions *CardUpdateOptions `json:"cardUpdateOptions"`
	CardData          *CardDataDto       `json:"cardData"`
	UserPrivateData   *CardDataDto       `json:"userPrivateData"`
}

func (r *CardRequest) GetActionString(name string) string {
	value, ok := r.CardActionData.CardPrivateData.Params[name]
	if !ok {
		return ""
	}
	s, ok := value.(string)
	if ok {
		return s
	}
	return ""
}
