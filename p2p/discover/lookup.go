// Copyright 2019 The go-ethereum Authors
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

package discover

import (
	"context"
	"time"

	"github.com/ethereum/go-ethereum/p2p/enode"
)

// lookup performs a network search for nodes close to the given target. It approaches the
// target by querying nodes that are closer to it on each iteration. The given target does
// not need to be an actual node identifier.
type lookup struct {
	tab *Table
	// 查询节点的函数
	queryfunc func(*node) ([]*node, error)
	// 用于临时存放从db中加载的和实际查询到的结果。后续还需要将其push到result中，我理解其实是解藕异步处理
	replyCh  chan []*node
	cancelCh <-chan struct{}
	// seen 被发现的节点。就不会被重复push到result
	// asked 已经询问的节点。就不会被发送请求进行询问
	asked, seen map[enode.ID]bool
	// 存放距离目标node由近到远的节点
	result nodesByDistance
	// 用于惰性查询时使用
	replyBuffer []*node
	queries     int
}

type queryFunc func(*node) ([]*node, error)

func newLookup(ctx context.Context, tab *Table, target enode.ID, q queryFunc) *lookup {
	it := &lookup{
		tab:       tab,
		queryfunc: q,
		asked:     make(map[enode.ID]bool),
		seen:      make(map[enode.ID]bool),
		result:    nodesByDistance{target: target},
		replyCh:   make(chan []*node, alpha),
		cancelCh:  ctx.Done(),
		queries:   -1,
	}
	// Don't query further if we hit ourself.
	// Unlikely to happen often in practice.
	it.asked[tab.self().ID()] = true
	return it
}

// run runs the lookup to completion and returns the closest nodes found.
func (it *lookup) run() []*enode.Node {
	// 返回 true 表示有发现了新的 node, false代表已经没有新的 node的了, 这一轮的lookup到此位置, 返回发现的node信息
	for it.advance() {
	}
	return unwrapNodes(it.result.entries)
}

// advance advances the lookup until any new nodes have been found.
// It returns false when the lookup has ended.
func (it *lookup) advance() bool {
	// 如果 queries == 0 就直接返回了。因为在这一轮没有发现新的节点
	for it.startQueries() {
		select {
		case nodes := <-it.replyCh:
			it.replyBuffer = it.replyBuffer[:0]
			for _, n := range nodes {
				if n != nil && !it.seen[n.ID()] {
					it.seen[n.ID()] = true
					it.result.push(n, bucketSize)
					it.replyBuffer = append(it.replyBuffer, n)
				}
			}
			it.queries--
			// 如果 replyBuffer > 0 说明有新的node。直接返回true。感觉这里设计的很绕
			if len(it.replyBuffer) > 0 {
				return true
			}
		case <-it.cancelCh:
			it.shutdown()
		}
	}
	return false
}

func (it *lookup) shutdown() {
	for it.queries > 0 {
		<-it.replyCh
		it.queries--
	}
	it.queryfunc = nil
	it.replyBuffer = nil
}

func (it *lookup) startQueries() bool {
	if it.queryfunc == nil {
		return false
	}

	// The first query returns nodes from the local table.
	// it.queries 一开始是-1，代表还没有开始。此时就会从 table 中获取节点信息（table中的节点信息来自leveldb或者节点启动配置）
	if it.queries == -1 {
		closest := it.tab.findnodeByID(it.result.target, bucketSize, false)
		// Avoid finishing the lookup too quickly if table is empty. It'd be better to wait
		// for the table to fill in this case, but there is no good mechanism for that
		// yet.
		if len(closest.entries) == 0 {
			it.slowdown()
		}
		it.queries = 1
		// 就算 entries == 0也会返回
		it.replyCh <- closest.entries
		return true
	}

	// 根据上面的 advance 可知。如果table中没有节点。那么第二轮来到这里的时候 it.queries = 0
	// Ask the closest nodes that we haven't asked yet.
	for i := 0; i < len(it.result.entries) && it.queries < alpha; i++ {
		n := it.result.entries[i]
		if !it.asked[n.ID()] {
			it.asked[n.ID()] = true
			it.queries++
			go it.query(n, it.replyCh)
		}
	}
	// The lookup ends when no more nodes can be asked.
	return it.queries > 0
}

func (it *lookup) slowdown() {
	sleep := time.NewTimer(1 * time.Second)
	defer sleep.Stop()
	select {
	case <-sleep.C:
	case <-it.tab.closeReq:
	}
}

func (it *lookup) query(n *node, reply chan<- []*node) {
	// 这里首先查一下leveldb记录的这个节点的查找失败记录（获取不到节点也算
	fails := it.tab.db.FindFails(n.ID(), n.IP())
	// 这里的 queryfunc 就是发一个 findnode 请求到目标节点查找他附近的节点
	r, err := it.queryfunc(n)
	if err == errClosed {
		// Avoid recording failures on shutdown.
		reply <- nil
		return
	} else if len(r) == 0 {
		// 如果获取不到节点，更新 fails 记录
		fails++
		it.tab.db.UpdateFindFails(n.ID(), n.IP(), fails)
		// Remove the node from the local table if it fails to return anything useful too
		// many times, but only if there are enough other nodes in the bucket.
		// 如果多次找不到对应的即节点。而且当前 bucket 的数量超过一半了，就直接把它踢了
		dropped := false
		if fails >= maxFindnodeFailures && it.tab.bucketLen(n.ID()) >= bucketSize/2 {
			dropped = true
			// 这里只是将它踢出缓存，并不会从 leveldb中删除
			it.tab.delete(n)
		}
		it.tab.log.Trace("FINDNODE failed", "id", n.ID(), "failcount", fails, "dropped", dropped, "err", err)
	} else if fails > 0 {
		// Reset failure counter because it counts _consecutive_ failures.
		it.tab.db.UpdateFindFails(n.ID(), n.IP(), 0)
	}

	// Grab as many nodes as possible. Some of them might not be alive anymore, but we'll
	// just remove those again during revalidation.
	// 如果有的话就直接加到 table 的bucket中。当然最终会写入到 leveldb
	for _, n := range r {
		it.tab.addSeenNode(n)
	}
	reply <- r
}

// lookupIterator performs lookup operations and iterates over all seen nodes.
// When a lookup finishes, a new one is created through nextLookup.
type lookupIterator struct {
	buffer     []*node
	nextLookup lookupFunc
	ctx        context.Context
	cancel     func()
	lookup     *lookup
}

type lookupFunc func(ctx context.Context) *lookup

func newLookupIterator(ctx context.Context, next lookupFunc) *lookupIterator {
	ctx, cancel := context.WithCancel(ctx)
	return &lookupIterator{ctx: ctx, cancel: cancel, nextLookup: next}
}

// Node returns the current node.
func (it *lookupIterator) Node() *enode.Node {
	if len(it.buffer) == 0 {
		return nil
	}
	return unwrapNode(it.buffer[0])
}

// Next moves to the next node.
func (it *lookupIterator) Next() bool {
	// Consume next node in buffer.
	if len(it.buffer) > 0 {
		it.buffer = it.buffer[1:]
	}
	// Advance the lookup to refill the buffer.
	for len(it.buffer) == 0 {
		if it.ctx.Err() != nil {
			it.lookup = nil
			it.buffer = nil
			return false
		}
		if it.lookup == nil {
			it.lookup = it.nextLookup(it.ctx)
			continue
		}
		if !it.lookup.advance() {
			it.lookup = nil
			continue
		}
		it.buffer = it.lookup.replyBuffer
	}
	return true
}

// Close ends the iterator.
func (it *lookupIterator) Close() {
	it.cancel()
}
