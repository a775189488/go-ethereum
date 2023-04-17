// Copyright 2021 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package types

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

//go:generate go run ../../rlp/rlpgen -type StateAccount -out gen_account_rlp.go

// StateAccount is the Ethereum consensus representation of accounts.
// These objects are stored in the main account trie.
type StateAccount struct {
	// 交易序列号，防止双花
	Nonce   uint64
	Balance *big.Int
	// Root 表示当前账户的下 Storage 层的 Merkle Patricia Trie 的 Root。这里的存储层是为了管理合约中持久化变量准备的。
	// 对于 EOA 账户这个部分为空值
	Root common.Hash // merkle root of the storage trie
	// CodeHash 是该账户的 Contract 代码的哈希值。同样的，这个变量是用于保存合约账户中的代码的 hash ，EOA 账户这个部分为空值。
	CodeHash []byte
}
