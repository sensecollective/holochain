package holochain

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	ic "github.com/libp2p/go-libp2p-crypto"
	net "github.com/libp2p/go-libp2p-net"
	peer "github.com/libp2p/go-libp2p-peer"
	pstore "github.com/libp2p/go-libp2p-peerstore"
	. "github.com/metacurrency/holochain/hash"
	. "github.com/smartystreets/goconvey/convey"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNodeDiscovery(t *testing.T) {
	nodesCount := 2
	mt := setupMultiNodeTesting(nodesCount)
	defer mt.cleanupMultiNodeTesting()
	nodes := mt.nodes

	node1 := nodes[0].node
	node2 := nodes[1].node

	Convey("nodes should find eachother via mdns", t, func() {
		So(len(node1.host.Peerstore().Peers()), ShouldEqual, 1)
		So(len(node2.host.Peerstore().Peers()), ShouldEqual, 1)

		err := node1.EnableMDNSDiscovery(nodes[0], time.Second/2)
		So(err, ShouldBeNil)
		err = node2.EnableMDNSDiscovery(nodes[1], time.Second/2)
		So(err, ShouldBeNil)

		time.Sleep(time.Second * 1)

		// many nodes from previous tests show up... TODO, figure out how to clear them
		So(len(node1.host.Peerstore().Peers()) > 1, ShouldBeTrue)
		So(len(node2.host.Peerstore().Peers()) > 1, ShouldBeTrue)
	})
}

func TestNewNode(t *testing.T) {

	node, err := makeNode(1234, "")
	defer node.Close()
	Convey("It should create a node", t, func() {
		So(err, ShouldBeNil)
		So(node.NetAddr.String(), ShouldEqual, "/ip4/127.0.0.1/tcp/1234")
		So(node.HashAddr.Pretty(), ShouldEqual, "QmNN6oDiV4GsfKDXPVmGLdBLLXCM28Jnm7pz7WD63aiwSG")
	})

	Convey("It should send between nodes", t, func() {
		node2, err := makeNode(4321, "node2")
		So(err, ShouldBeNil)
		defer node2.Close()

		node.host.Peerstore().AddAddr(node2.HashAddr, node2.NetAddr, pstore.PermanentAddrTTL)
		var payload string
		node2.host.SetStreamHandler("/testprotocol/1.0.0", func(s net.Stream) {
			defer s.Close()

			buf := make([]byte, 1024)
			n, err := s.Read(buf)
			if err != nil {
				payload = err.Error()
			} else {
				payload = string(buf[:n])
			}

			_, err = s.Write([]byte("I got: " + payload))

			if err != nil {
				panic(err)
			}
		})

		s, err := node.host.NewStream(node.ctx, node2.HashAddr, "/testprotocol/1.0.0")
		So(err, ShouldBeNil)
		_, err = s.Write([]byte("greetings"))
		So(err, ShouldBeNil)

		out, err := ioutil.ReadAll(s)
		So(err, ShouldBeNil)
		So(payload, ShouldEqual, "greetings")
		So(string(out), ShouldEqual, "I got: greetings")
	})
}

func TestNewMessage(t *testing.T) {
	node, err := makeNode(1234, "node1")
	if err != nil {
		panic(err)
	}
	defer node.Close()
	Convey("It should create a new message", t, func() {
		now := time.Now()
		m := node.NewMessage(PUT_REQUEST, "fish")
		So(now.Before(m.Time), ShouldBeTrue) // poor check, but at least makes sure the time was set to something just after the NewMessage call was made
		So(m.Type, ShouldEqual, PUT_REQUEST)
		So(m.Body, ShouldEqual, "fish")
		So(m.From, ShouldEqual, node.HashAddr)
	})
}

func TestNodeSend(t *testing.T) {
	d, _, h := PrepareTestChain("test")
	defer CleanupTestChain(h, d)

	node1, err := makeNode(1234, "node1")
	if err != nil {
		panic(err)
	}
	h.node.Close()
	h.node = node1

	d2, _, h2 := PrepareTestChain("test2")
	defer CleanupTestChain(h2, d2)
	h2.node.Close()

	node2, err := makeNode(1235, "node2")
	if err != nil {
		panic(err)
	}
	defer node2.Close()
	h2.node = node2
	os.Remove(filepath.Join(h2.DBPath(), DHTStoreFileName))
	h2.dht = NewDHT(h2)

	h.Activate()

	Convey("It should start the DHT protocols", t, func() {
		err := h2.dht.Start()
		So(err, ShouldBeNil)
	})
	Convey("It should start the Nucleus protocols", t, func() {
		err := h2.nucleus.Start()
		So(err, ShouldBeNil)
	})

	node2.host.Peerstore().AddAddr(node1.HashAddr, node1.NetAddr, pstore.PermanentAddrTTL)

	Convey("It should fail on messages without a source", t, func() {
		m := Message{Type: PUT_REQUEST, Body: "fish"}
		So(len(node1.host.Peerstore().Peers()), ShouldEqual, 1)
		r, err := node2.Send(context.Background(), ActionProtocol, node1.HashAddr, &m)
		So(err, ShouldBeNil)
		So(len(node1.host.Peerstore().Peers()), ShouldEqual, 2) // node1's peerstore should now have node2
		So(r.Type, ShouldEqual, ERROR_RESPONSE)
		So(r.From, ShouldEqual, node1.HashAddr) // response comes from who we sent to
		So(r.Body.(ErrorResponse).Message, ShouldEqual, "message must have a source")
	})

	Convey("It should fail on incorrect message types", t, func() {
		m := node1.NewMessage(PUT_REQUEST, "fish")
		r, err := node1.Send(context.Background(), ValidateProtocol, node2.HashAddr, m)
		So(err, ShouldBeNil)
		So(r.Type, ShouldEqual, ERROR_RESPONSE)
		So(r.From, ShouldEqual, node2.HashAddr) // response comes from who we sent to
		So(r.Body.(ErrorResponse).Message, ShouldEqual, "message type 2 not in holochain-validate protocol")

		m = node2.NewMessage(PUT_REQUEST, "fish")
		r, err = node2.Send(context.Background(), GossipProtocol, node1.HashAddr, m)
		So(err, ShouldBeNil)
		So(r.Type, ShouldEqual, ERROR_RESPONSE)
		So(r.From, ShouldEqual, node1.HashAddr) // response comes from who we sent to
		So(r.Body.(ErrorResponse).Message, ShouldEqual, "message type 2 not in holochain-gossip protocol")

		m = node2.NewMessage(GOSSIP_REQUEST, "fish")
		r, err = node2.Send(context.Background(), ActionProtocol, node1.HashAddr, m)
		So(err, ShouldBeNil)
		So(r.Type, ShouldEqual, ERROR_RESPONSE)
		So(r.From, ShouldEqual, node1.HashAddr) // response comes from who we sent to
		So(r.Body.(ErrorResponse).Message, ShouldEqual, "message type 9 not in holochain-action protocol")

	})

	Convey("It should respond with err on bad request on invalid PUT_REQUESTS", t, func() {
		hash, _ := NewHash("QmY8Mzg9F69e5P9AoQPYat6x5HEhc1TVGs11tmfNSzkqh2")

		m := node2.NewMessage(PUT_REQUEST, PutReq{H: hash})
		r, err := node2.Send(context.Background(), ActionProtocol, node1.HashAddr, m)
		So(err, ShouldBeNil)
		So(r.Type, ShouldEqual, ERROR_RESPONSE)
		So(r.From, ShouldEqual, node1.HashAddr) // response comes from who we sent to
		So(r.Body.(ErrorResponse).Code, ShouldEqual, ErrHashNotFoundCode)
	})

	Convey("It should respond with OK if valid request", t, func() {
		m := node2.NewMessage(GOSSIP_REQUEST, GossipReq{})
		r, err := node2.Send(context.Background(), GossipProtocol, node1.HashAddr, m)
		So(err, ShouldBeNil)
		So(r.Type, ShouldEqual, OK_RESPONSE)
		So(r.From, ShouldEqual, node1.HashAddr) // response comes from who we sent to
		So(fmt.Sprintf("%T", r.Body), ShouldEqual, "holochain.Gossip")
	})

	Convey("it should respond with err on messages from nodes on the blockedlist", t, func() {
		node1.Block(node2.HashAddr)
		m := node2.NewMessage(GOSSIP_REQUEST, GossipReq{})
		r, err := node2.Send(context.Background(), GossipProtocol, node1.HashAddr, m)
		So(err, ShouldBeNil)
		So(r.Type, ShouldEqual, ERROR_RESPONSE)
		So(r.From, ShouldEqual, node1.HashAddr) // response comes from who we sent to
		So(r.Body.(ErrorResponse).Code, ShouldEqual, ErrBlockedListedCode)
	})

	Convey("it should respond with err on messages to nodes on the blockedlist", t, func() {
		node1.Block(node2.HashAddr)
		m := node1.NewMessage(GOSSIP_REQUEST, GossipReq{})
		_, err = node1.Send(context.Background(), GossipProtocol, node2.HashAddr, m)
		So(err, ShouldEqual, ErrBlockedListed)
	})

}

func TestNodeBlockedList(t *testing.T) {
	Convey("it should be set up from a peerlist", t, func() {
		node, _ := makeNode(1234, "node1")
		node2, _ := makeNode(1235, "node2")

		So(node.IsBlocked(node2.HashAddr), ShouldBeFalse)
		node.InitBlockedList(PeerList{Records: []PeerRecord{PeerRecord{ID: node2.HashAddr}}})
		So(node.IsBlocked(node2.HashAddr), ShouldBeTrue)
		node.Unblock(node2.HashAddr)
		So(node.IsBlocked(node2.HashAddr), ShouldBeFalse)
		node.Block(node2.HashAddr)
		So(node.IsBlocked(node2.HashAddr), ShouldBeTrue)
	})
}

func TestMessageCoding(t *testing.T) {
	node, err := makeNode(1234, "node1")
	if err != nil {
		panic(err)
	}
	defer node.Close()

	m := node.NewMessage(PUT_REQUEST, "foo")
	var d []byte
	Convey("It should encode and decode put messages", t, func() {
		d, err = m.Encode()
		So(err, ShouldBeNil)

		var m2 Message
		r := bytes.NewReader(d)
		err = m2.Decode(r)
		So(err, ShouldBeNil)

		So(fmt.Sprintf("%v", m), ShouldEqual, fmt.Sprintf("%v", &m2))
	})

	m = node.NewMessage(GET_REQUEST, "foo")
	Convey("It should encode and decode get messages", t, func() {
		d, err = m.Encode()
		So(err, ShouldBeNil)

		var m2 Message
		r := bytes.NewReader(d)
		err = m2.Decode(r)
		So(err, ShouldBeNil)

		So(fmt.Sprintf("%v", m), ShouldEqual, fmt.Sprintf("%v", &m2))
	})

	Convey("It should encode and decode get OK_RESPONSE", t, func() {
		body := GetResp{}
		body.Entry = GobEntry{C: "3"}

		m = node.NewMessage(OK_RESPONSE, body)
		d, err = m.Encode()
		So(err, ShouldBeNil)

		var m2 Message
		r := bytes.NewReader(d)
		err = m2.Decode(r)
		So(err, ShouldBeNil)

		So(fmt.Sprintf("%v", m), ShouldEqual, fmt.Sprintf("%v", &m2))
	})

}

func TestFingerprintMessage(t *testing.T) {
	Convey("it should create a unique fingerprint for messages", t, func() {
		var id peer.ID
		var mp *Message
		f, err := mp.Fingerprint()
		So(err, ShouldBeNil)
		So(f.String(), ShouldEqual, NullHash().String())
		now := time.Unix(1, 1) // pick a constant time so the test will always work
		m := Message{Type: PUT_REQUEST, Time: now, Body: "foo", From: id}
		f, err = m.Fingerprint()
		So(err, ShouldBeNil)
		So(f.String(), ShouldEqual, "QmTZf2qqYiKbJbQVpFyidMVyAtb1S4xQNV52LcX9LDVTQn")
		m = Message{Type: PUT_REQUEST, Time: now, Body: "foo1", From: id}
		f, err = m.Fingerprint()
		So(err, ShouldBeNil)
		So(f.String(), ShouldEqual, "QmP2WUSMWAuZrX2nqWcEyei7GDCwVaetkynQESFDrHNkGa")
		now = time.Unix(1, 2) // pick a constant time so the test will always work
		m = Message{Type: PUT_REQUEST, Time: now, Body: "foo", From: id}
		f, err = m.Fingerprint()
		So(err, ShouldBeNil)
		So(f.String(), ShouldEqual, "QmTZf2qqYiKbJbQVpFyidMVyAtb1S4xQNV52LcX9LDVTQn")
		m = Message{Type: GET_REQUEST, Time: now, Body: "foo", From: id}
		f, err = m.Fingerprint()
		So(err, ShouldBeNil)
		So(f.String(), ShouldEqual, "Qmd7v7bxE7xRCj3Amhx8kyj7DbUGJdbKzuiUUahx3ARPec")
	})
}

func TestErrorCoding(t *testing.T) {
	Convey("it should encode and decode errors", t, func() {
		er := NewErrorResponse(ErrHashNotFound)
		So(er.DecodeResponseError(), ShouldEqual, ErrHashNotFound)
		er = NewErrorResponse(ErrHashDeleted)
		So(er.DecodeResponseError(), ShouldEqual, ErrHashDeleted)
		er = NewErrorResponse(ErrHashModified)
		So(er.DecodeResponseError(), ShouldEqual, ErrHashModified)
		er = NewErrorResponse(ErrHashRejected)
		So(er.DecodeResponseError(), ShouldEqual, ErrHashRejected)
		er = NewErrorResponse(ErrLinkNotFound)
		So(er.DecodeResponseError(), ShouldEqual, ErrLinkNotFound)

		er = NewErrorResponse(errors.New("Some Error"))
		So(er.Code, ShouldEqual, ErrUnknownCode)
		So(er.DecodeResponseError().Error(), ShouldEqual, "Some Error")
	})
}

func TestAddPeer(t *testing.T) {
	d, _, h := PrepareTestChain("test")
	defer CleanupTestChain(h, d)
	somePeer, _ := makePeer("some peer")
	Convey("it should add a peer to the peer store and the gossip list", t, func() {
		So(h.node.routingTable.Size(), ShouldEqual, 0)
		So(len(h.node.peerstore.Peers()), ShouldEqual, 1)
		err := h.AddPeer(somePeer, nil)
		So(err, ShouldBeNil)
		So(len(h.node.peerstore.Peers()), ShouldEqual, 2)
		glist, err := h.dht.getGossipers()
		So(err, ShouldBeNil)
		So(len(glist), ShouldEqual, 1)
		So(glist[0], ShouldEqual, somePeer)
		So(h.node.routingTable.Size(), ShouldEqual, 1)
	})

	Convey("it should not add a blocked peer", t, func() {
		blockedPeer, _ := makePeer("blocked peer")
		h.node.Block(blockedPeer)
		err := h.AddPeer(blockedPeer, nil)
		So(err, ShouldEqual, ErrBlockedListed)
		So(len(h.node.peerstore.Peers()), ShouldEqual, 2)
		glist, err := h.dht.getGossipers()
		So(err, ShouldBeNil)
		So(len(glist), ShouldEqual, 1)
		So(glist[0], ShouldEqual, somePeer)
		So(h.node.routingTable.Size(), ShouldEqual, 1)
	})
}

func TestNodeRouting(t *testing.T) {
	d, _, h := PrepareTestChain("test")
	defer CleanupTestChain(h, d)
	node := h.node

	start := 0
	testPeerCount := 20
	peers := []peer.ID{}

	peers = addTestPeers(h, peers, start, testPeerCount)
	Convey("populating routing", t, func() {
		p := node.HashAddr
		srch := node.routingTable.NearestPeers(HashFromPeerID(p), 5)
		nearest := fmt.Sprintf("%d %v", len(srch), srch)
		So(nearest, ShouldEqual, "5 [<peer.ID P9vKpw> <peer.ID P9QXqa> <peer.ID PrUBh5> <peer.ID Pn94bj> <peer.ID QHFWTH>]")
		start = testPeerCount
		testPeerCount += 5
		peers = addTestPeers(h, peers, start, testPeerCount)
		srch = node.routingTable.NearestPeers(HashFromPeerID(p), 5)
		nearest = fmt.Sprintf("%d %v", len(srch), srch)

		// adding a few more yields one which is closer
		So(nearest, ShouldEqual, "5 [<peer.ID NSQqJR> <peer.ID P9vKpw> <peer.ID P9QXqa> <peer.ID PrUBh5> <peer.ID Pn94bj>]")
		//		node.routingTable.Print()
	})
}

func TestNodeAppSendResolution(t *testing.T) {
	nodesCount := 50
	mt := setupMultiNodeTesting(nodesCount)
	defer mt.cleanupMultiNodeTesting()
	ringConnect(t, mt.ctx, mt.nodes, nodesCount)
	node1 := mt.nodes[0].node
	node2 := mt.nodes[nodesCount/2].node

	Convey("sending to nodes we aren't directly connected to should resolve", t, func() {
		m := node2.NewMessage(GOSSIP_REQUEST, GossipReq{})
		r, err := node2.Send(mt.ctx, GossipProtocol, node1.HashAddr, m)
		So(err, ShouldBeNil)
		So(r.Type, ShouldEqual, OK_RESPONSE)
		So(r.From, ShouldEqual, node1.HashAddr) // response comes from who we sent to
		So(fmt.Sprintf("%T", r.Body), ShouldEqual, "holochain.Gossip")
	})
}

func makePeer(id string) (pid peer.ID, key ic.PrivKey) {
	// use a constant reader so the key will be the same each time for the test...
	var err error
	key, _, err = ic.GenerateEd25519Key(makeTestSeed(id))
	if err != nil {
		panic(err)
	}
	pid, _ = peer.IDFromPrivateKey(key)
	return
}

func makeNode(port int, id string) (*Node, error) {
	listenaddr := fmt.Sprintf("/ip4/127.0.0.1/tcp/%d", port)
	_, key := makePeer(id)
	agent := LibP2PAgent{identity: AgentIdentity(id), priv: key, pub: key.GetPublic()}
	return NewNode(listenaddr, "fakednahash", &agent, false)
}

func addTestPeers(h *Holochain, peers []peer.ID, start int, count int) []peer.ID {
	for i := start; i < count; i++ {
		p, _ := makePeer(fmt.Sprintf("peer_%d", i))
		//		fmt.Printf("Peer %d: %s\n", i, peer.IDB58Encode(p))
		peers = append(peers, p)
		err := h.AddPeer(p, nil)
		if err != nil {
			panic(err)
		}
	}
	return peers
}
