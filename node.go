// Copyright (C) 2013-2017, The MetaCurrency Project (Eric Harris-Braun, Arthur Brock, et. al.)
// Use of this source code is governed by GPLv3 found in the LICENSE file
//----------------------------------------------------------------------------------------

// node implements ipfs network transport for communicating between holochain nodes

package holochain

import (
	"context"
	//	host "github.com/libp2p/go-libp2p-host"
	"encoding/gob"
	"errors"
	"fmt"

	goprocess "github.com/jbenet/goprocess"
	goprocessctx "github.com/jbenet/goprocess/context"

	nat "github.com/libp2p/go-libp2p-nat"

	net "github.com/libp2p/go-libp2p-net"
	peer "github.com/libp2p/go-libp2p-peer"
	pstore "github.com/libp2p/go-libp2p-peerstore"
	protocol "github.com/libp2p/go-libp2p-protocol"
	swarm "github.com/libp2p/go-libp2p-swarm"
	bhost "github.com/libp2p/go-libp2p/p2p/host/basic"
	rhost "github.com/libp2p/go-libp2p/p2p/host/routed"
	"github.com/metacurrency/holochain/discovery"
	. "github.com/metacurrency/holochain/hash"
	ma "github.com/multiformats/go-multiaddr"
	mh "github.com/multiformats/go-multihash"
	"gopkg.in/mgo.v2/bson"
	"io"

	"math/big"
	"sync"

	go_net "net"
	"strconv"
	"strings"

	"time"
)

type ReceiverFn func(h *Holochain, m *Message) (response interface{}, err error)

type MsgType int8

const (
	// common messages

	ERROR_RESPONSE MsgType = iota
	OK_RESPONSE

	// DHT messages

	PUT_REQUEST
	DEL_REQUEST
	MOD_REQUEST
	GET_REQUEST
	LINK_REQUEST
	GETLINK_REQUEST
	DELETELINK_REQUEST

	// Gossip messages

	GOSSIP_REQUEST

	// Validate Messages

	VALIDATE_PUT_REQUEST
	VALIDATE_LINK_REQUEST
	VALIDATE_DEL_REQUEST
	VALIDATE_MOD_REQUEST

	// Application Messages

	APP_MESSAGE

	// Peer messages

	LISTADD_REQUEST

	// Kademlia messages

	FIND_NODE_REQUEST
)

func (msgType MsgType) String() string {
	return []string{"ERROR_RESPONSE",
		"OK_RESPONSE",
		"PUT_REQUEST",
		"DEL_REQUEST",
		"MOD_REQUEST",
		"GET_REQUEST",
		"LINK_REQUEST",
		"GETLINK_REQUEST",
		"DELETELINK_REQUEST",
		"GOSSIP_REQUEST",
		"VALIDATE_PUT_REQUEST",
		"VALIDATE_LINK_REQUEST",
		"VALIDATE_DEL_REQUEST",
		"VALIDATE_MOD_REQUEST",
		"APP_MESSAGE",
		"LISTADD_REQUEST",
		"FIND_NODE_REQUEST"}[msgType]
}

var ErrBlockedListed = errors.New("node blockedlisted")

// Message represents data that can be sent to node in the network
type Message struct {
	Type MsgType
	Time time.Time
	From peer.ID
	Body interface{}
}

// Node represents a node in the network
type Node struct {
	HashAddr     peer.ID
	NetAddr      ma.Multiaddr
	host         *rhost.RoutedHost
	mdnsSvc      discovery.Service
	blockedlist  map[peer.ID]bool
	protocols    [_protocolCount]*Protocol
	peerstore    pstore.Peerstore
	routingTable *RoutingTable
	nat          *nat.NAT

	// items for the kademlia implementation
	plk   sync.Mutex
	peers map[peer.ID]*peerTracker
	ctx   context.Context
	proc  goprocess.Process
}

// Protocol encapsulates data for our different protocols
type Protocol struct {
	ID       protocol.ID
	Receiver ReceiverFn
}

const (
	ActionProtocol = iota
	ValidateProtocol
	GossipProtocol
	KademliaProtocol
	_protocolCount
)

const (
	PeerTTL = time.Minute * 10
)

// implement peer found function for mdns discovery
func (h *Holochain) HandlePeerFound(pi pstore.PeerInfo) {
	h.dht.dlog.Logf("discovered peer via mdns: %v", pi)
	err := h.AddPeer(pi.ID, pi.Addrs)
	if err != nil {
		h.dht.dlog.Logf("error when adding peer: %v", pi)
	}
}

func (h *Holochain) AddPeer(id peer.ID, addrs []ma.Multiaddr) (err error) {
	if h.node.IsBlocked(id) {
		err = ErrBlockedListed
	} else {
		Debugf("Adding Peer: %v\n", id)
		h.node.peerstore.AddAddrs(id, addrs, PeerTTL)
		h.node.routingTable.Update(id)
		err = h.dht.AddGossiper(id)
	}
	return
}

func (n *Node) EnableMDNSDiscovery(h *Holochain, interval time.Duration) (err error) {
	ctx := context.Background()
	tag := h.dnaHash.String() + "._udp"
	n.mdnsSvc, err = discovery.NewMdnsService(ctx, n.host, interval, tag)
	if err != nil {
		return
	}
	n.mdnsSvc.RegisterNotifee(h)
	return
}

func (n *Node) ExternalAddr() ma.Multiaddr {
	if n.nat == nil {
		return n.NetAddr
	} else {
		mappings := n.nat.Mappings()
		for i := 0; i < len(mappings); i++ {
			external_addr, err := mappings[i].ExternalAddr()
			if err == nil {
				return external_addr
			}
		}
		return n.NetAddr
	}
}

func (n *Node) discoverAndHandleNat(listenPort int) {
	Debugf("Looking for a NAT...")
	n.nat = nat.DiscoverNAT()
	if n.nat == nil {
		Debugf("No NAT found.")
	} else {
		Debugf("Discovered NAT! Trying to aquire public port mapping via UPnP...")
		ifaces, _ := go_net.Interfaces()
		// handle err
		for _, i := range ifaces {
			addrs, _ := i.Addrs()
			// handle err
			for _, addr := range addrs {
				var ip go_net.IP
				switch v := addr.(type) {
				case *go_net.IPNet:
					ip = v.IP
				case *go_net.IPAddr:
					ip = v.IP
				}
				if ip.Equal(go_net.IPv4(127, 0, 0, 1)) {
					continue
				}
				addr_string := fmt.Sprintf("/ip4/%s/tcp/%d", ip, listenPort)
				localaddr, err := ma.NewMultiaddr(addr_string)
				if err == nil {
					Debugf("NAT: trying to establish NAT mapping for %s...", addr_string)
					n.nat.NewMapping(localaddr)
				}
			}
		}

		external_addr := n.ExternalAddr()

		if external_addr != n.NetAddr {
			Debugf("NAT: successfully created port mapping! External address is: %s", external_addr.String())
		} else {
			Debugf("NAT: could not create port mappping. Keep trying...")
			Infof("NAT:-------------------------------------------------------")
			Infof("NAT:---------------------Warning---------------------------")
			Infof("NAT:-------------------------------------------------------")
			Infof("NAT: You seem to be behind a NAT that does not speak UPnP.")
			Infof("NAT: You will have to setup a port forwarding manually.")
			Infof("NAT: This instance is configured to listen on port: %d", listenPort)
			Infof("NAT:-------------------------------------------------------")
		}

	}
}

// NewNode creates a new node with given multiAddress listener string and identity
func NewNode(listenAddr string, protoMux string, agent *LibP2PAgent, enableNATUPnP bool) (node *Node, err error) {
	Debugf("Creating new node with protoMux: %s\n", protoMux)
	nodeID, _, err := agent.NodeID()
	if err != nil {
		return
	}

	var n Node
	listenPort, err := strconv.Atoi(strings.Split(listenAddr, "/")[4])
	if err != nil {
		Infof("Can't parse port from Multiaddress string: %s", listenAddr)
		return
	}

	n.NetAddr, err = ma.NewMultiaddr(listenAddr)
	if err != nil {
		return
	}

	if enableNATUPnP {
		n.discoverAndHandleNat(listenPort)
	}

	ps := pstore.NewPeerstore()
	n.peerstore = ps
	ps.AddAddrs(nodeID, []ma.Multiaddr{n.NetAddr}, pstore.PermanentAddrTTL)

	n.HashAddr = nodeID
	priv := agent.PrivKey()
	ps.AddPrivKey(nodeID, priv)
	ps.AddPubKey(nodeID, priv.GetPublic())

	validateProtocolString := "/hc-validate-" + protoMux + "/0.0.0"
	gossipProtocolString := "/hc-gossip-" + protoMux + "/0.0.0"
	actionProtocolString := "/hc-action-" + protoMux + "/0.0.0"
	kademliaProtocolString := "/hc-kademlia-" + protoMux + "/0.0.0"

	Debugf("Validate protocol identifier: " + validateProtocolString)
	Debugf("Gossip protocol identifier: " + gossipProtocolString)
	Debugf("Action protocol identifier: " + actionProtocolString)
	Debugf("Kademlia protocol identifier: " + kademliaProtocolString)

	n.protocols[ValidateProtocol] = &Protocol{protocol.ID(validateProtocolString), ValidateReceiver}
	n.protocols[GossipProtocol] = &Protocol{protocol.ID(gossipProtocolString), GossipReceiver}
	n.protocols[ActionProtocol] = &Protocol{protocol.ID(actionProtocolString), ActionReceiver}
	n.protocols[KademliaProtocol] = &Protocol{protocol.ID(kademliaProtocolString), KademliaReceiver}

	ctx := context.Background()
	n.ctx = ctx

	// create a new swarm to be used by the service host
	netw, err := swarm.NewNetwork(ctx, []ma.Multiaddr{n.NetAddr}, nodeID, ps, nil)
	if err != nil {
		return nil, err
	}

	var bh *bhost.BasicHost
	bh, err = bhost.New(netw), nil
	if err != nil {
		return
	}

	n.host = rhost.Wrap(bh, &n)

	m := pstore.NewMetrics()
	n.routingTable = NewRoutingTable(KValue, nodeID, time.Minute, m)
	n.peers = make(map[peer.ID]*peerTracker)

	node = &n

	n.host.Network().Notify((*netNotifiee)(node))

	n.proc = goprocessctx.WithContextAndTeardown(ctx, func() error {
		// remove ourselves from network notifs.
		n.host.Network().StopNotify((*netNotifiee)(node))
		return n.host.Close()
	})

	return
}

// Encode codes a message to gob format
// @TODO generalize for other message encoding formats
func (m *Message) Encode() (data []byte, err error) {
	data, err = ByteEncoder(m)
	if err != nil {
		return
	}
	return
}

// Decode converts a message from gob format
// @TODO generalize for other message encoding formats
func (m *Message) Decode(r io.Reader) (err error) {
	dec := gob.NewDecoder(r)
	err = dec.Decode(m)
	return
}

// Fingerprint creates a hash of a message
func (m *Message) Fingerprint() (f Hash, err error) {
	var data []byte
	if m != nil {
		data, err = bson.Marshal(m)

		if err != nil {
			return
		}
		f.H, err = mh.Sum(data, mh.SHA2_256, -1)
	} else {
		f = NullHash()
	}

	return
}

// String converts a message to a nice string
func (m Message) String() string {
	return fmt.Sprintf("%v @ %v From:%v Body:%v", m.Type, m.Time, m.From, m.Body)
}

// respondWith writes a message either error or otherwise, to the stream
func (node *Node) respondWith(s net.Stream, err error, body interface{}) {
	var m *Message
	if err != nil {
		errResp := NewErrorResponse(err)
		errResp.Payload = body
		m = node.NewMessage(ERROR_RESPONSE, errResp)
	} else {
		m = node.NewMessage(OK_RESPONSE, body)
	}

	data, err := m.Encode()
	if err != nil {
		Infof("Response failed: unable to encode message: %v", m)
	}
	_, err = s.Write(data)
	if err != nil {
		Infof("Response failed: write returned error: %v", err)
	}
}

// StartProtocol initiates listening for a protocol on the node
func (node *Node) StartProtocol(h *Holochain, proto int) (err error) {
	node.host.SetStreamHandler(node.protocols[proto].ID, func(s net.Stream) {
		var m Message
		err := m.Decode(s)
		var response interface{}
		if m.From == "" {
			// @todo other sanity checks on From?
			err = errors.New("message must have a source")
		} else {
			if node.IsBlocked(s.Conn().RemotePeer()) {
				err = ErrBlockedListed
			}

			if err == nil {
				response, err = node.protocols[proto].Receiver(h, &m)
			}
		}
		node.respondWith(s, err, response)
	})
	return
}

// Close shuts down the node
func (node *Node) Close() error {
	return node.proc.Close()
}

// Send delivers a message to a node via the given protocol
func (node *Node) Send(ctx context.Context, proto int, addr peer.ID, m *Message) (response Message, err error) {

	if node.IsBlocked(addr) {
		err = ErrBlockedListed
		return
	}

	s, err := node.host.NewStream(ctx, addr, node.protocols[proto].ID)
	if err != nil {
		return
	}
	defer s.Close()

	// encode the message and send it
	data, err := m.Encode()
	if err != nil {
		return
	}

	n, err := s.Write(data)
	if err != nil {
		return
	}
	if n != len(data) {
		err = errors.New("unable to send all data")
	}

	// decode the response
	err = response.Decode(s)
	if err != nil {
		Debugf("failed to decode: %v err:%v ", err)
		return
	}
	return
}

// NewMessage creates a message from the node with a new current timestamp
func (node *Node) NewMessage(t MsgType, body interface{}) (msg *Message) {
	m := Message{Type: t, Time: time.Now().Round(0), Body: body, From: node.HashAddr}
	msg = &m
	return
}

// IsBlockedListed checks to see if a node is on the blockedlist
func (node *Node) IsBlocked(addr peer.ID) (ok bool) {
	ok = node.blockedlist[addr]
	return
}

// InitBlockedList sets up the blockedlist from a PeerList
func (node *Node) InitBlockedList(list PeerList) {
	node.blockedlist = make(map[peer.ID]bool)
	for _, r := range list.Records {
		node.Block(r.ID)
	}
}

// Block adds a peer to the blocklist
func (node *Node) Block(addr peer.ID) {
	if node.blockedlist == nil {
		node.blockedlist = make(map[peer.ID]bool)
	}
	node.blockedlist[addr] = true
}

// Unblock removes a peer from the blocklist
func (node *Node) Unblock(addr peer.ID) {
	if node.blockedlist != nil {
		delete(node.blockedlist, addr)
	}
}

type ErrorResponse struct {
	Code    int
	Message string
	Payload interface{}
}

const (
	ErrUnknownCode = iota
	ErrHashNotFoundCode
	ErrHashDeletedCode
	ErrHashModifiedCode
	ErrHashRejectedCode
	ErrLinkNotFoundCode
	ErrEntryTypeMismatchCode
	ErrBlockedListedCode
)

// NewErrorResponse encodes standard errors for transmitting
func NewErrorResponse(err error) (errResp ErrorResponse) {
	switch err {
	case ErrHashNotFound:
		errResp.Code = ErrHashNotFoundCode
	case ErrHashDeleted:
		errResp.Code = ErrHashDeletedCode
	case ErrHashModified:
		errResp.Code = ErrHashModifiedCode
	case ErrHashRejected:
		errResp.Code = ErrHashRejectedCode
	case ErrLinkNotFound:
		errResp.Code = ErrLinkNotFoundCode
	case ErrEntryTypeMismatch:
		errResp.Code = ErrEntryTypeMismatchCode
	case ErrBlockedListed:
		errResp.Code = ErrBlockedListedCode
	default:
		errResp.Message = err.Error() //Code will be set to ErrUnknown by default cus it's 0
	}
	return
}

// DecodeResponseError creates a go error object from the ErrorResponse data
func (errResp ErrorResponse) DecodeResponseError() (err error) {
	switch errResp.Code {
	case ErrHashNotFoundCode:
		err = ErrHashNotFound
	case ErrHashDeletedCode:
		err = ErrHashDeleted
	case ErrHashModifiedCode:
		err = ErrHashModified
	case ErrHashRejectedCode:
		err = ErrHashRejected
	case ErrLinkNotFoundCode:
		err = ErrLinkNotFound
	case ErrEntryTypeMismatchCode:
		err = ErrEntryTypeMismatch
	case ErrBlockedListedCode:
		err = ErrBlockedListed
	default:
		err = errors.New(errResp.Message)
	}
	return
}

// Distance returns the nodes peer distance to another node for purposes of gossip
func (node *Node) Distance(id peer.ID) *big.Int {
	h := HashFromPeerID(id)
	nh := HashFromPeerID(node.HashAddr)
	return HashXORDistance(nh, h)
}

// Context return node's context
func (node *Node) Context() context.Context {
	return node.ctx
}

// Process return node's process
func (node *Node) Process() goprocess.Process {
	return node.proc
}
