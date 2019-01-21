/*
 * @file
 * @copyright defined in aergo/LICENSE.txt
 */

// Code generated by mockery v1.0.0. DO NOT EDIT.
package p2p

import mock "github.com/stretchr/testify/mock"
import types "github.com/aergoio/aergo/types"

// MockChainAccessor is an autogenerated mock type for the MockChainAccessor type
type MockChainAccessor struct {
	mock.Mock
}

// GetBestBlock provides a mock function with given fields:
func (_m *MockChainAccessor) GetBestBlock() (*types.Block, error) {
	ret := _m.Called()

	var r0 *types.Block
	if rf, ok := ret.Get(0).(func() *types.Block); ok {
		r0 = rf()
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*types.Block)
		}
	}

	var r1 error
	if rf, ok := ret.Get(1).(func() error); ok {
		r1 = rf()
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// GetBlock should provides a genesis block info.
func (_m *MockChainAccessor) GetGenesisInfo() *types.Genesis {
	// Not implemented since it is not used in any test.
	return nil
}

// GetBlock provides a mock function with given fields: blockHash
func (_m *MockChainAccessor) GetBlock(blockHash []byte) (*types.Block, error) {
	ret := _m.Called(blockHash)

	var r0 *types.Block
	if rf, ok := ret.Get(0).(func([]byte) *types.Block); ok {
		r0 = rf(blockHash)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*types.Block)
		}
	}

	var r1 error
	if rf, ok := ret.Get(1).(func([]byte) error); ok {
		r1 = rf(blockHash)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// GetBlock provides a mock function with given fields: blockHash
func (_m *MockChainAccessor) GetHashByNo(blockNo types.BlockNo) ([]byte, error) {
	ret := _m.Called(blockNo)

	var r0 []byte
	if rf, ok := ret.Get(0).(func(types.BlockNo) []byte); ok {
		r0 = rf(blockNo)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).([]byte)
		}
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(types.BlockNo) error); ok {
		r1 = rf(blockNo)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}
