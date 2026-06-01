package domain

type User struct {
	UserID       uint64 `json:"userId" protobuf:"fixed64,1,opt,name=userId"`
	Name         string `json:"name" protobuf:"bytes,2,opt,name=name"`
	Username     string `json:"username" protobuf:"bytes,3,opt,name=username"`
	PasswordHash string `json:"-" protobuf:"bytes,4,opt,name=passwordHash"`
}

type Category struct {
	CategoryID uint64  `json:"categoryId" protobuf:"fixed64,1,opt,name=categoryId"`
	Name       string  `json:"name" protobuf:"bytes,2,opt,name=name"`
	ParentID   *uint64 `json:"parentId,omitempty" protobuf:"fixed64,3,opt,name=parentId"`
	SortOrder  int32   `json:"sortOrder" protobuf:"varint,4,opt,name=sortOrder"`
}

type Merchant struct {
	MerchantID uint64 `json:"merchantId" protobuf:"fixed64,1,opt,name=merchantId"`
	Name       string `json:"name" protobuf:"bytes,2,opt,name=name"`
}

type Item struct {
	ItemID     uint64   `json:"itemId" protobuf:"fixed64,1,opt,name=itemId"`
	Name       string   `json:"name" protobuf:"bytes,2,opt,name=name"`
	CategoryID uint64   `json:"categoryId" protobuf:"fixed64,3,opt,name=categoryId"`
	MerchantID uint64   `json:"merchantId" protobuf:"fixed64,4,opt,name=merchantId"`
	Normalized string   `json:"normalized" protobuf:"bytes,5,opt,name=normalized"`
	Aliases    []string `json:"aliases,omitempty" protobuf:"bytes,6,rep,name=aliases"`
}

type Receipt struct {
	ReceiptID  uint64        `json:"receiptId" protobuf:"fixed64,1,opt,name=receiptId"`
	MerchantID uint64        `json:"merchantId" protobuf:"fixed64,2,opt,name=merchantId"`
	OwnerID    uint64        `json:"ownerId" protobuf:"fixed64,3,opt,name=ownerId"`
	Date       int64         `json:"date" protobuf:"fixed64,4,opt,name=date"`
	PhotoURL   string        `json:"photoUrl,omitempty" protobuf:"bytes,5,opt,name=photoUrl"`
	Items      []ReceiptItem `json:"items" protobuf:"bytes,6,rep,name=items"`
	Total      float64       `json:"total" protobuf:"fixed64,7,opt,name=total"`
}

type ReceiptItem struct {
	ItemID    uint64  `json:"itemId" protobuf:"fixed64,1,opt,name=itemId"`
	Quantity  uint32  `json:"quantity" protobuf:"varint,2,opt,name=quantity"`
	UnitPrice float64 `json:"unitPrice" protobuf:"fixed64,3,opt,name=unitPrice"`
}

type Proposal struct {
	ProposalID uint64         `json:"proposalId" protobuf:"fixed64,1,opt,name=proposalId"`
	OwnerID    uint64         `json:"ownerId" protobuf:"fixed64,2,opt,name=ownerId"`
	Merchant   string         `json:"merchant" protobuf:"bytes,3,opt,name=merchant"`
	Date       int64          `json:"date" protobuf:"fixed64,4,opt,name=date"`
	PhotoURL   string         `json:"photoUrl,omitempty" protobuf:"bytes,5,opt,name=photoUrl"`
	Items      []ProposalItem `json:"items" protobuf:"bytes,6,rep,name=items"`
	Total      float64        `json:"total" protobuf:"fixed64,7,opt,name=total"`
	Status     string         `json:"status" protobuf:"bytes,8,opt,name=status"`
}

type ProposalItem struct {
	ParsedName    string  `json:"parsedName" protobuf:"bytes,1,opt,name=parsedName"`
	Quantity      uint32  `json:"quantity" protobuf:"varint,2,opt,name=quantity"`
	UnitPrice     float64 `json:"unitPrice" protobuf:"fixed64,3,opt,name=unitPrice"`
	MatchedItemID uint64  `json:"matchedItemId,omitempty" protobuf:"fixed64,4,opt,name=matchedItemId"`
	Confidence    float64 `json:"confidence" protobuf:"fixed64,5,opt,name=confidence"`
	CategoryID    uint64  `json:"categoryId,omitempty" protobuf:"fixed64,6,opt,name=categoryId"`
	IsNewCategory bool    `json:"isNewCategory,omitempty" protobuf:"varint,7,opt,name=isNewCategory"`
	UserChoice    string  `json:"userChoice,omitempty" protobuf:"bytes,8,opt,name=userChoice"`
}
