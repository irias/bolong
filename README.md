backup - tool for making backups, and restoring them

# features

- incremental and full backups
- can store data in aws s3 or local filesystem (possibly network storage).  cloud storage is nice: highly available, pay for use, off-site.  we clean up old files too
- data is compressed, encrypted, authenticated
- focus on performance: waiting for a restore can be troublesome

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


plan of attack:
- then do incremental
- then add some cli flags or config file to make usage easier
- then encryption
- then cli flags for include/exclude
- then cleaning up old backups
- then compression
- done
