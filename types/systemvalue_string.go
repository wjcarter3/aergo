// Code generated by "stringer -type=SystemValue"; DO NOT EDIT.

package types

import "strconv"

func _() {
	// An "invalid array index" compiler error signifies that the constant values have changed.
	// Re-run the stringer command to generate them again.
	var x [1]struct{}
	_ = x[StakingTotal-0]
	_ = x[StakingMin-1]
	_ = x[GasPrice-2]
	_ = x[NamePrice-3]
}

const _SystemValue_name = "StakingTotalStakingMinGasPriceNamePrice"

var _SystemValue_index = [...]uint8{0, 12, 22, 30, 39}

func (i SystemValue) String() string {
	if i < 0 || i >= SystemValue(len(_SystemValue_index)-1) {
		return "SystemValue(" + strconv.FormatInt(int64(i), 10) + ")"
	}
	return _SystemValue_name[_SystemValue_index[i]:_SystemValue_index[i+1]]
}