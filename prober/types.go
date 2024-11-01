package prober

type ABIInput struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type ABIOutput struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// Define ABI struct
type ABIMethod struct {
	Name    string      `json:"name"`
	Type    string      `json:"type"`
	Inputs  []ABIInput  `json:"inputs,omitempty"`
	Outputs []ABIOutput `json:"outputs,omitempty"`
}
