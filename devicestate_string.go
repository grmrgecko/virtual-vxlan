// Code generated by "stringer -type deviceState -trimprefix=deviceState"; DO NOT EDIT.

package main

import "strconv"

func _() {
	// An "invalid array index" compiler error signifies that the constant values have changed.
	// Re-run the stringer command to generate them again.
	var x [1]struct{}
	_ = x[deviceStateDown-0]
	_ = x[deviceStateUp-1]
	_ = x[deviceStateClosed-2]
}

const _deviceState_name = "DownUpClosed"

var _deviceState_index = [...]uint8{0, 4, 6, 12}

func (i deviceState) String() string {
	if i >= deviceState(len(_deviceState_index)-1) {
		return "deviceState(" + strconv.FormatInt(int64(i), 10) + ")"
	}
	return _deviceState_name[_deviceState_index[i]:_deviceState_index[i+1]]
}
