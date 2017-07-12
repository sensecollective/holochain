using Go = import "/go.capnp";
using import "entry.capnp".Entry;
@0xf5d8b05955f7ad6f;
$Go.package("wireformat");
$Go.import("holochain/wireformat");


enum StatusMask {
	default  	@0;
	live		 	@1;
	rejected 	@2;
	deleted		@3;
	modified	@4;
	any				@5;
}

enum GetMask {
	default 	@0;
	entry			@1;
	entryType @2;
	sources		@3;
	all				@4;
}

# PutRequest holds the data of a put request
struct PutRequest {
	hash @0 :Text;
}

# GetRequest holds the data of a get request
struct GetRequest {
	hash @0 :Text;
	statusMask @1 :StatusMask;
	getMask @2 :GetMask;
}

# GetResponse holds the data of a get response
struct GetResponse {
	entry 			@0 :Entry;
	sources 		@1 :List(Text);
	followHash 	@2 :Text; # hash of new entry if the entry was modified and needs following
}

# DelRequest holds the data of a del request
struct DelRequest {
	hash 	@0 :Text; # hash to be deleted
	by		@1 :Text; # hash of DelEntry on source chain took this action
}

# ModRequest holds the data of a mod request
struct ModRequest {
	oldHash @0 :Text;
	newHash @1 :Text;
}

# LinkRequest holds a link request
struct LinkRequest {
	base  @0 :Text; # data on which to attach the links
	links @1 :Text; # hash of the links entry
}

# DelLinkRequest holds a delete link request
struct DelLinkRequest {
	base @0 :Text; # data on which to link was attached
	link @1 :Text; # hash of the link entry
	tag  @2 :Text; # tag to be deleted
}

# LinkQuery holds a getLink query
struct LinkQuery {
	base 				@0 :Text;
	tag 				@1 :Text;
	statusMask 	@2 :StatusMask;
}
