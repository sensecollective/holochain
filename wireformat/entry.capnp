using Go = import "/go.capnp";
@0x959f5b6b81f1e7f7;
$Go.package("wireformat");
$Go.import("holochain/wireformat");

struct Entry {
	union {
      customContent :union {
        json @0 :Text;
        bson @1 :Data;
      }
      agentContent @2 :AgentEntry;
      linksContent @3 :LinksEntry;
      delContent @4 :DelEntry;
	}

  # AgentEntry structure for building KeyEntryType entries
  struct AgentEntry {
    name      @0 :Text;
    keyType   @1 :Int8;
    publicKey @2 :Text;
  }

  # LinksEntry holds one or more links
  struct LinksEntry {
    links @0 :List(Link);
  }

  # Link structure for holding meta tagging of linking entry
  struct Link {
    linkAction  @0 :Text; # StatusAction (either AddAction or ModAction)
    base        @1 :Text; # hash of entry (perhaps elsewhere) tow which we are attaching the link
    link        @2 :Text; # hash of entry being linked to
    tag         @3 :Text; # tag
  }

  # DelEntry struct holds the record of an entry's deletion
  struct DelEntry {
    hash    @0 :Text;
    message @1 :Text;
  }
}
