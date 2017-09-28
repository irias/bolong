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



name.incr.index
name.incr.data
name.full.index
name.full.data

list dir
get all the names.
should be in incremental order when sorted by name. so just timestamped.
backup id's are the name-part.
just looking at the files tells you what which files would need to be retrieved.
the .index files have a full listing you would get when doing an extraction, including permission, if it's a dir, username/groupname.
index file is encrypted too.
index file contains offset into data file.
so data is just all the content appended.
when restoring, we can fetch the data streaming, and write the necessary files as we go.  we begin at the newest file.  we might not even need have to retrieve the full data file.

example index file:

path/to d 755 1506578834 0 mjl mjl 0
path/to/file f 644 1506578834 1234 mjl mjl 0
path/to/another-file f 644 1506578834 100 mjl mjl 1234
path/to/another-file f 644 1506578834 123123123 mjl mjl 1334

"." is included
".." cannot occur as path element
paths cannot start with a slash
paths are normalized to contain just one slash


plan of attack:

- implement doing full backup and restore.  no cli flags yet.  no incremental yet, no compression or encryption.
- then list command
- then do incremental
- then encryption
- then cli flags for include/exclude
- then cleaning up old backups
- then compression
- done
