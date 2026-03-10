package pdfform

type FieldInfo struct {
	Name         string      `json:"name"`
	Kind         string      `json:"kind"`
	ToolTip      string      `json:"toolTip,omitempty"`
	ReadOnly     bool        `json:"readOnly"`
	Required     bool        `json:"required"`
	CurrentValue interface{} `json:"currentValue,omitempty"`
	Choices      []string    `json:"choices,omitempty"`
}

type SchemaFieldInfo struct {
	Name     string   `json:"name"`
	Kind     string   `json:"kind"`
	ToolTip  string   `json:"toolTip,omitempty"`
	ReadOnly bool     `json:"readOnly"`
	Required bool     `json:"required"`
	Choices  []string `json:"choices,omitempty"`
}

type Inspection struct {
	PDFPath                     string      `json:"pdfPath"`
	FormType                    string      `json:"formType"`
	IsXfaForm                   bool        `json:"isXfaForm"`
	IsSupportedAcroForm         bool        `json:"isSupportedAcroForm"`
	CanFillValues               bool        `json:"canFillValues"`
	SupportedFillableFieldCount int         `json:"supportedFillableFieldCount"`
	ValidationMessage           string      `json:"validationMessage"`
	FieldCount                  int         `json:"fieldCount"`
	Fields                      []FieldInfo `json:"fields"`
}

type Schema struct {
	PDFPath                     string            `json:"pdfPath"`
	FormType                    string            `json:"formType"`
	IsXfaForm                   bool              `json:"isXfaForm"`
	IsSupportedAcroForm         bool              `json:"isSupportedAcroForm"`
	CanFillValues               bool              `json:"canFillValues"`
	SupportedFillableFieldCount int               `json:"supportedFillableFieldCount"`
	ValidationMessage           string            `json:"validationMessage"`
	FieldCount                  int               `json:"fieldCount"`
	Fields                      []SchemaFieldInfo `json:"fields"`
}

type FillRequest struct {
	PDFPath    string
	ValuesPath string
	OutputPath string
	Flatten    bool
}

type FillResult struct {
	PDFPath         string   `json:"pdfPath"`
	OutputPath      string   `json:"outputPath"`
	FormType        string   `json:"formType"`
	Flattened       bool     `json:"flattened"`
	AppliedFields   int      `json:"appliedFields"`
	SkippedFields   []string `json:"skippedFields"`
	UnusedInputKeys []string `json:"unusedInputKeys"`
}
