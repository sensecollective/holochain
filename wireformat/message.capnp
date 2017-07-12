
using Go = import "/go.capnp";
using Dht = import "dht_messages.capnp";
@0x8b6f325f59887a6b;
$Go.package("wireformat");
$Go.import("holochain/wireformat");

enum MessageType {
	# common messages
	errorResponse @0;
	okResponse @1;

	# DHT messages
	dhtPutRequest @2;
	dhtDelRequest @3;
	dhtModRequest @4;
	dhtGetRequest @5;
	dhtLinkRequest @6;
	dhtGetLinkRequest @7;
	dhtDeleteLinkRequest @8;

	# Gossip messages
	gossipRequest @9;

	# Validate Messages
	validatePutRequest @10;
	validateLinkRequest @11;
	validateDelRequst @12;
	validateModRequest @13;
}

struct Time {
	unixSec @0 : Int64;
	unixNsec @1 : Int64;
}

struct Message {
	#type @0 : MessageType;
	time @6 : Time;
	from @7 : Text;

	union {
		putReq 			@0 :Dht.PutRequest;
		getReq 			@1 :Dht.GetRequest;
		delReq 			@2 :Dht.DelRequest;
		modReq 			@3 :Dht.ModRequest;
		linkReq 		@4 :Dht.LinkRequest;
		delLinkReq 	@5 :Dht.DelLinkRequest;
	}
}
