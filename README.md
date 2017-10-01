backup - tool for making backups, and restoring them

# features

- incremental and full backups. incremental backups based on timestamp + filesize (not contents). we store permissions, user/group and permissions.
- can store data in aws s3 or local filesystem (possibly network storage).  cloud storage is nice: highly available, pay for use, off-site.  we clean up old files too, so you won't keep paying more and more for cloud storage.
- data is compressed, encrypted, authenticated
- focus on performance: waiting for a restore can be troublesome. we can do streaming restore/backup from/to the cloud.  each backup creates a single data store, so (re)storing many small files is quick and doesn't suffer from high latency.

# non-features

- we don't do deduplication. quite a bit more complicated to implement. too much code for this tool.


# example usage

backup -remote /n/backups -key backup.key list
backup -remote /n/backups -key backup.key backup -exclude '*.jpg' -incremental 30 -max-full 12 -max-incremental 2 .
backup -remote /n/backups -key backup.key restore -exclude '*.png' <backup-id> .



name.index.incr
name.data
name.index.full
name.data

list dir
get all the names.
should be in incremental order when sorted by name. so just timestamped.
backup id's are the name-part.
just looking at the files tells you what which files would need to be retrieved.
the index files have a full listing you would get when doing an extraction, including permission, if it's a dir, username/groupname.
index file is encrypted too.
index file contains offset into data file.
so data is just all the content appended.
when restoring, we can fetch the data streaming, and write the necessary files as we go.  we begin at the newest file.  we might not even need have to retrieve the full data file.

example index file:

index0
20170101-122334
- path/removed
+ path/to/file
= d 755 1506578834 0 mjl mjl 0 path/to
= f 644 1506578834 1234 mjl mjl 0 path/to/file
= f 644 1506578834 100 mjl mjl 1234 path/to/another-file
= f 644 1506578834 123123123 mjl mjl 1334 path/to/another-file
.


"." is included
".." cannot occur as path element
paths cannot start with a slash
paths are normalized to contain just one slash


incremental backups:
- read last index
- walk through tree
- remove paths that are gone
- add files that are new/modified
- keep track of the additions/removals, also put them in the index file?
- also put the filename of the previous index file in the new index file

when restoring incremental backups:
- read the new index file.  gives us list of files we will need.  keep of track of the work we still need to do.  once empty, we quit.
- go through the contents, restore all files the that were added in this version.  if no more work, quit. otherwise, read the next index file and restore all added files that are still in the work list.


compression:
we use lz4. it is very fast. ratio isn't always great, but this way we don't have to make it complicated (turning it on/off per type of file), and we never slow the backup/restore down.  native go lib available: github.com/pierrec/lz4

encryption:
we don't use pub/priv key stuff. means we would need to keep those keys around, annoying. instead we'll do a passphrase with key derivation.
which key derivation function?  pbkdf2, scrypt, hkdf.  pbkdf2. do we need a salt? store it at the front of the index file.

version
method
salt
iv
data...


plan of attack:
- then s3 support
- then cli flags for include/exclude and the incremental/full intervals
- then cleaning up old backups
- done
