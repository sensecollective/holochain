// Copyright (C) 2013-2017, The MetaCurrency Project (Eric Harris-Braun, Arthur Brock, et. al.)
// Use of this source code is governed by GPLv3 found in the LICENSE file
//----------------------------------------------------------------------------------------
// Service implements functions and data that provide Holochain services

package entangler
import (
//  . "holochain"
  "github.com/metacurrency/holochain/wireformat"
)

//type GetEntanglement struct {
//  Hash       Hash
//  StatusMask int
//  GetMask    int
//}

func (g* GetRequest) Who(dht *DHT, chain *Chain, nucleus *Nucleus) peer.ID{
    return dht.FindNodeForHash(g.GetHash())
}

func (g* GetRequest) AddToCapnpMessage(message wireformat.Message) error {
  return message.setGetReq(g)
}

func (g* GetEntanglement) ReadFromCapnp(msg *capnp.Message) {
  message, err := wireformat.ReadRootMessage(msg)

}

func (g* GetRequest) SpinUp(dht *DHT, chain *Chain, nucleus *Nucleus) (t MsgType, body interface{}) {
  t = GET_REQUEST
  body = GetReq{
    H:          g.Hash,
    StatusMask: g.StatusMask,
    GetMask:    g.GetMask,
  }
  return
}

func (g* GetEntanglement) Entangle(message *Message, dht *DHT, chain *Chain, nucleus *Nucleus) (response interface{}, err error){
  //message.GetGetReq
  var entryData []byte
	//var status int
	req := message.Body.(GetReq)
	mask := req.GetMask
	if mask == GetMaskDefault {
		mask = GetMaskEntry
	}
	resp := GetResp{}
	var entryType string
	entryData, entryType, resp.Sources, _, err = dht.get(req.H, req.StatusMask, req.GetMask|GetMaskEntryType)
	if (mask & GetMaskEntryType) != 0 {
		resp.EntryType = entryType
	}

	if err == nil {
		if (mask & GetMaskEntry) != 0 {
			switch entryType {
			case DNAEntryType:
				panic("nobody should actually get the DNA!")
			case AgentEntryType:
				fallthrough
			case KeyEntryType:
				var e GobEntry
				e.C = string(entryData)
				resp.Entry = &e
			default:
				var e GobEntry
				err = e.Unmarshal(entryData)
				if err != nil {
					return
				}
				resp.Entry = &e
			}
		}
	} else {
		if err == ErrHashModified {
      // this relies on the fact that if the entry was modified
      // the DHT get call will return the hash of the new entry
      // instead of the old entry's data
			resp.FollowHash = string(entryData)
		}
	}
	response = resp
	return
}

func (g* GetEntanglement) SpinDown(response interface{}, dht *DHT, chain *Chain, nucleus *Nucleus) {
  switch t := response.(type) {
  case GetResp:
    response = t
  default:
    err = fmt.Errorf("expected GetResp response from GET_REQUEST, got: %T", t)
    return
  }

  // follow the modified hash
  if g.StatusMask == StatusDefault && err == ErrHashModified {
    var hash Hash
    hash, err = NewHash(response.(GetResp).FollowHash)
    if err != nil {
      return
    }
    req := GetReq{H: hash, StatusMask: StatusDefault, GetMask: g.GetMask}
    modResp, err := NewGetAction(req, a.options).Do(dht.h)
    if err == nil {
      response = modResp
    }
  }
  return
}
