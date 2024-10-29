package dto

type AuthResponseDTO struct {
	UserID    string `json:"userId"`
	AccountID string `json:"accountId"`
	AccessKey string `json:"accessKey"`
	Token     string `json:"token"`
	IsValid   bool   `json:"isValid"`
	Roles     struct {
		Administration Role `json:"administration"`
		Function       Role `json:"function"`
		Credential     Role `json:"credential"`
		Execute        Role `json:"execute"`
		Policy         Role `json:"policy"`
	} `json:"roles"`
}

type Role struct {
	Read  bool `json:"read"`
	Write bool `json:"write"`
}
