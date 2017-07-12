// Copyright (C) 2013-2017, The MetaCurrency Project (Eric Harris-Braun, Arthur Brock, et. al.)
// Use of this source code is governed by GPLv3 found in the LICENSE file
//----------------------------------------------------------------------------------------
// Service implements functions and data that provide Holochain services

package entangler

import (
	peer "github.com/libp2p/go-libp2p-peer"
	//	. "github.com/metacurrency/holochain"
	"github.com/metacurrency/holochain/wireformat"
	"sync"
)

type Entanglement interface {
	//ReadFromMessage(message Message)
	Who(dht *DHT, chain *Chain, nucleus *Nucleus) peer.ID
	//BuildMessage() Message
	SpinUp(dht *DHT, chain *Chain, nucleus *Nucleus) (body interface{})
	Entangle(message Message, dht *DHT, chain *Chain, nucleus *Nucleus) Response
	SpinDown(response Response, dht *DHT, chain *Chain, nucleus *Nucleus)
}

type EntanglementMaker func(seg *capnp.Segment) interface{}

type Entangler struct {
	dht      *DHT
	chain    *Chain
	nucleus  *Nucleus
	thisNode *Node
}

var entangle_instance *Entangler
var once sync.Once

func MakeInstance(h *Holochain) {
	once.Do(func() {
		instance = &Entanglements{
			dht:      h.dht,
			chain:    h.chain,
			nucleus:  h.nucleus,
			thisNode: h.node,
		}
	})
}

func GetEntangler() *Entangler {
	return entangle_instance
}

func Receiver(_ *Holochain, msg *Message) (response interface{}, err error) {
	// Read the message from stdin.
	msg, err := capnp.NewDecoder(reader).Decode()
	if err != nil {
		panic(err)
	}

	// Extract the root struct from the message.
	message, err := wireformat.ReadRootMessage(msg)
	if err != nil {
		panic(err)
	}

	// Access fields from the struct.
	title, err := book.Title()
	if err != nil {
		panic(err)
	}
}

// MakeActionFromMessage generates an action from an action protocol messsage
func MakeEntanglementFromMessage(msg *Message) (e Entanglement, err error) {
	var t reflect.Type
	switch msg.Type {
	case GET_REQUEST:
		msg.
			e = &GetEntanglement{}
		t = reflect.TypeOf(GetReq{})
	default:
		err = fmt.Errorf("message type %d not in entangler protocol", int(msg.Type))
	}
	if err == nil && reflect.TypeOf(msg.Body) != t {
		err = fmt.Errorf("Unexpected request body type '%T' in %s request, expecting %v", msg.Body, a.Name(), t)
	}
	return
}

func serializeWithCapnp(entanglement Entanglement) []byte {
	// Make a brand new empty message.  A Message allocates Cap'n Proto structs.
	msg, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
	if err != nil {
		panic(err)
	}

	entanglement.WriteToCapnp(msg, seg)

}

func (e *Entangler) Execute(entanglementMaker EntanglementMaker) promise.Future {
	responsePromise := promise.NewPromise()

	go func() {
		msg, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
		if err != nil {
			panic(err)
		}

		message, err := wireformat.NewRootMessage(seg)
		entanglement := entanglementMaker(seg)
		entanglement.SpinUp(e.dht, e.chain, e.nucleus)
		entanglement.AddToCapnpMessage(message)

		// if we are sending to ourselves we should bypass the network mechanics and call
		// the receiver directly
		if to == e.thisNode.HashAddr {
			Debugf("Sending message (local):%v (fingerprint:%s)", message, f)
			response, err = entanglement.Entangle(message, e.dht, e.chain, e.nucleus)
			Debugf("send result (local): %v (fp:%s)error:%v", response, f, err)
		} else {
			Debugf("Sending message (net):%v (fingerprint:%s)", message, f)

			buffer := bytes.NewBuffer
			err = capnp.NewEncoder(buffer).Encode(msg)
			if err != nil {
				panic(err)
			}

			to := entanglement.Who()
			message := e.thisNode.SendRaw(EntanglerProtocol, to, buffer,
				func(reader io.Reader) error {
					// Read the message from stdin.
					msg, err := capnp.NewDecoder(reader).Decode()
					if err != nil {
						panic(err)
					}

					// Extract the root struct from the message.
					message, err := wireformat.ReadRootMessage(msg)
					if err != nil {
						panic(err)
					}

					// Access fields from the struct.
					title, err := book.Title()
					if err != nil {
						panic(err)
					}
				})

			f, err := message.Fingerprint()
			if err != nil {
				panic(fmt.Sprintf("error calculating fingerprint when sending message %v", message))
			}

			var r Message
			r, err = e.thisNode.Send(EntanglementProtocol, to, message)
			Debugf("send result (net): %v (fp:%s) error:%v", r, f, err)

			if err == nil {
				if r.Type == ERROR_RESPONSE {
					errResp := r.Body.(ErrorResponse)
					err = errResp.DecodeResponseError()
					response = errResp.Payload
				} else {
					response = r.Body
				}
			}
		}

		err = entanglement.SpinDown(response, err, e.dht, e.chain, e.nucleus)

		if err != nil {
			responsePromise.Reject(err)
		} else {
			responsePromise.Resolve(response)
		}
	}()

	return responsePromise.Future
}
