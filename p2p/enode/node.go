// Copyright 2018 The go-ethereum Authors
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

package enode

import (
	"crypto/ecdsa"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"math/bits"
	"net"
	"strings"

	"github.com/ethereum/go-ethereum/p2p/enr"
	"github.com/ethereum/go-ethereum/rlp"
)

var errMissingPrefix = errors.New("missing 'enr:' prefix for base64-encoded record")

// Node represents a host on the network.
type Node struct {
	// 存储了节点信息，包括ip地址，协议，公钥等
	r  enr.Record
	id ID
}

// New wraps a node record. The record must be valid according to the given
// identity scheme.
func New(validSchemes enr.IdentityScheme, r *enr.Record) (*Node, error) {
	if err := r.VerifySignature(validSchemes); err != nil {
		return nil, err
	}
	node := &Node{r: *r}
	// id = validSchemes.NodeAddr(&node.r) localnode的初始方式是使用IDV4
	// 所以这里的 id 其实是 public key 加密后的值
	if n := copy(node.id[:], validSchemes.NodeAddr(&node.r)); n != len(ID{}) {
		return nil, fmt.Errorf("invalid node ID length %d, need %d", n, len(ID{}))
	}
	return node, nil
}

// MustParse parses a node record or enode:// URL. It panics if the input is invalid.
func MustParse(rawurl string) *Node {
	n, err := Parse(ValidSchemes, rawurl)
	if err != nil {
		panic("invalid node: " + err.Error())
	}
	return n
}

// Parse decodes and verifies a base64-encoded node record.
func Parse(validSchemes enr.IdentityScheme, input string) (*Node, error) {
	if strings.HasPrefix(input, "enode://") {
		return ParseV4(input)
	}
	if !strings.HasPrefix(input, "enr:") {
		return nil, errMissingPrefix
	}
	bin, err := base64.RawURLEncoding.DecodeString(input[4:])
	if err != nil {
		return nil, err
	}
	var r enr.Record
	if err := rlp.DecodeBytes(bin, &r); err != nil {
		return nil, err
	}
	return New(validSchemes, &r)
}

// ID returns the node identifier.
func (n *Node) ID() ID {
	return n.id
}

// Seq returns the sequence number of the underlying record.
func (n *Node) Seq() uint64 {
	return n.r.Seq()
}

// Incomplete returns true for nodes with no IP address.
func (n *Node) Incomplete() bool {
	return n.IP() == nil
}

// Load retrieves an entry from the underlying record.
func (n *Node) Load(k enr.Entry) error {
	return n.r.Load(k)
}

// IP returns the IP address of the node. This prefers IPv4 addresses.
func (n *Node) IP() net.IP {
	var (
		ip4 enr.IPv4
		ip6 enr.IPv6
	)
	if n.Load(&ip4) == nil {
		return net.IP(ip4)
	}
	if n.Load(&ip6) == nil {
		return net.IP(ip6)
	}
	return nil
}

// UDP returns the UDP port of the node.
func (n *Node) UDP() int {
	var port enr.UDP
	n.Load(&port)
	return int(port)
}

// TCP returns the TCP port of the node.
func (n *Node) TCP() int {
	var port enr.TCP
	n.Load(&port)
	return int(port)
}

// Pubkey returns the secp256k1 public key of the node, if present.
func (n *Node) Pubkey() *ecdsa.PublicKey {
	var key ecdsa.PublicKey
	if n.Load((*Secp256k1)(&key)) != nil {
		return nil
	}
	return &key
}

// Record returns the node's record. The return value is a copy and may
// be modified by the caller.
func (n *Node) Record() *enr.Record {
	cpy := n.r
	return &cpy
}

// ValidateComplete checks whether n has a valid IP and UDP port.
// Deprecated: don't use this method.
func (n *Node) ValidateComplete() error {
	if n.Incomplete() {
		return errors.New("missing IP address")
	}
	if n.UDP() == 0 {
		return errors.New("missing UDP port")
	}
	ip := n.IP()
	if ip.IsMulticast() || ip.IsUnspecified() {
		return errors.New("invalid IP (multicast/unspecified)")
	}
	// Validate the node key (on curve, etc.).
	var key Secp256k1
	return n.Load(&key)
}

// String returns the text representation of the record.
func (n *Node) String() string {
	if isNewV4(n) {
		return n.URLv4() // backwards-compatibility glue for NewV4 nodes
	}
	enc, _ := rlp.EncodeToBytes(&n.r) // always succeeds because record is valid
	b64 := base64.RawURLEncoding.EncodeToString(enc)
	return "enr:" + b64
}

// MarshalText implements encoding.TextMarshaler.
func (n *Node) MarshalText() ([]byte, error) {
	return []byte(n.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (n *Node) UnmarshalText(text []byte) error {
	dec, err := Parse(ValidSchemes, string(text))
	if err == nil {
		*n = *dec
	}
	return err
}

// ID is a unique identifier for each node.
type ID [32]byte

// Bytes returns a byte slice representation of the ID
func (n ID) Bytes() []byte {
	return n[:]
}

// ID prints as a long hexadecimal number.
func (n ID) String() string {
	return fmt.Sprintf("%x", n[:])
}

// The Go syntax representation of a ID is a call to HexID.
func (n ID) GoString() string {
	return fmt.Sprintf("enode.HexID(\"%x\")", n[:])
}

// TerminalString returns a shortened hex string for terminal logging.
func (n ID) TerminalString() string {
	return hex.EncodeToString(n[:8])
}

// MarshalText implements the encoding.TextMarshaler interface.
func (n ID) MarshalText() ([]byte, error) {
	return []byte(hex.EncodeToString(n[:])), nil
}

// UnmarshalText implements the encoding.TextUnmarshaler interface.
func (n *ID) UnmarshalText(text []byte) error {
	id, err := ParseID(string(text))
	if err != nil {
		return err
	}
	*n = id
	return nil
}

// HexID converts a hex string to an ID.
// The string may be prefixed with 0x.
// It panics if the string is not a valid ID.
func HexID(in string) ID {
	id, err := ParseID(in)
	if err != nil {
		panic(err)
	}
	return id
}

func ParseID(in string) (ID, error) {
	var id ID
	b, err := hex.DecodeString(strings.TrimPrefix(in, "0x"))
	if err != nil {
		return id, err
	} else if len(b) != len(id) {
		return id, fmt.Errorf("wrong length, want %d hex chars", len(id)*2)
	}
	copy(id[:], b)
	return id, nil
}

// DistCmp compares the distances a->target and b->target.
// Returns -1 if a is closer to target, 1 if b is closer to target
// and 0 if they are equal.
// ^ 操作是同真异假。所以我理解这个表达式其实也是直接比较 byte 数组的差异，并没有特别复杂的算法
// 根据 Kademlia 协议介绍。节点之间的距离越近，意味着节点ID的公共前缀越长
func DistCmp(target, a, b ID) int {
	for i := range target {
		da := a[i] ^ target[i]
		db := b[i] ^ target[i]
		if da > db {
			return 1
		} else if da < db {
			return -1
		}
	}
	return 0
}

// LogDist returns the logarithmic distance between a and b, log2(a ^ b).
// 返回两个节点的距离。 todo 没明白这里为什么注释说i使用 log2(a^b) 来计算距离。好像没什么关系
func LogDist(a, b ID) int {
	lz := 0
	// 这里其实就是求 a[i] ^ b[i] 的结果有多少个前导0并累加到 lz 变量中(换句话说就是求a和b的相同的位数～)
	//  由于 byte 是8位，那么如果 x = 0 。则就是有8个前导0
	for i := range a {
		x := a[i] ^ b[i]
		// 同真异假
		if x == 0 {
			lz += 8
		} else {
			// LeadingZeros8 这个函数其实就是求 uint8的前导0个数。如果 x != 0 说明这个byte已经不同了，就不需要再算下去了
			lz += bits.LeadingZeros8(x)
			break
		}
	}
	// len(a) * 8 实际为 aID 的总位数
	// lz 实际位aID和bID的前置相同的位数
	// 那么这里返回的其实就是aID和bID除了前置相同的位数之后的位数
	return len(a)*8 - lz
}
