package main

import (
	"fmt"

	"github.com/google/gopacket"
)

// A packet layer that just contains pre-computed bytes.
type Masquerade struct {
	MData      []byte
	MLayerType gopacket.LayerType
}

// Return the layer type of this layer that we're masquerading.
func (m *Masquerade) LayerType() gopacket.LayerType {
	return m.MLayerType
}

// Encode data to buffer.
func (m *Masquerade) SerializeTo(b gopacket.SerializeBuffer, opts gopacket.SerializeOptions) error {
	// Get the data length, and ensure there is data.
	length := len(m.MData)
	if length < 1 {
		return fmt.Errorf("invalid data")
	}

	// Allocate bytes in buffer for this data.
	bytes, err := b.PrependBytes(length)
	if err != nil {
		return err
	}

	// copy data into bytes.
	copy(bytes, m.MData)
	return nil
}
