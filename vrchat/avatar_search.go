package vrchat

import (
	"bytes"
	"encoding/json"
	"errors"
)

// SearchAvatarsResult accepts both avatar search response shapes. Avatars holds
// the result list. TotalCount and HasNext are present when metadata was requested.
type SearchAvatarsResult struct {
	Avatars    []Avatar `json:"results"`
	TotalCount *int     `json:"totalCount,omitempty"`
	HasNext    *bool    `json:"hasNext,omitempty"`
}

func (r *SearchAvatarsResult) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	var result SearchAvatarsResult
	if len(data) == 0 {
		return errors.New("empty VRChat avatar search response")
	}
	switch data[0] {
	case '[':
		if err := json.Unmarshal(data, &result.Avatars); err != nil {
			return err
		}
	case '{':
		type metadataResult SearchAvatarsResult
		if err := json.Unmarshal(data, (*metadataResult)(&result)); err != nil {
			return err
		}
		if result.Avatars == nil {
			return errors.New("VRChat avatar search response is missing results")
		}
	default:
		return errors.New("unexpected VRChat avatar search response")
	}
	*r = result
	return nil
}
