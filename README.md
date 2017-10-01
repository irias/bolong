Bolong is a simple, secure and fast command-line backup and restore tool.

Features:
- Full and incremental backups. You can configure how many incremental backups are made before a full backup is created. Incremental backups only store files that have different size/mtime/permissions compared to the previous backup. We don't compare the file contents.
- Stores data either in the "local" file system (which can be a mounted network disk) or in Google's S3 storage clone (not AWS, only Google does proper streaming uploads).
- Compression with lz4. Compression rate is not too great, but it's very fast, so won't slow restores down.
- Encryption and authenticated data. A cloud storage provider cannot read your data, and cannot tamper with it.

Non-features:
- Deduplication. It's a nice feature, but too much code/complexity for our purposes. Simple backups are more likely to be reliable backups.


# Examples

First, create a config file to your liking, named ".bolong.json". By default, we look in the current directory a file by that name, trying the parent directory, and its parent etc, until it finds one. Here is an example:

	{
		"kind": "googles3",
		"googles3": {
			"accessKey": "GOOGLTEST123456789",
			"secret": "bm90IGEgcmVhbCBrZXkuIG5pY2UgdHJ5IHRob3VnaCBeXg==",
			"bucket": "your-bucket-name",
			"path": "/"
		},
		"incrementalsPerFull": 6,
		"fullKeep": 8,
		"incrementalForFullKeep": 4,
		"passphrase": "She0oghoairie2Tu"
	}

For a more complete example, see bolong-example.json.txt.

Now we can create a new backup of the current directory:

	bolong backup

If all is well, it just worked, nothing is printed. If you are running these commands manually, you might want to add the "-verbose" flag. So you can see what is backed up.

Next, list the available backups:

	bolong list

Finally, we can restore one of the available backups. By default, the latest backup is restored:

	bolong restore /path/to/restore/to

Again, add the "-verbose" flag for a list of files restored.


# Compression

We use lz4 to compress all data. It so fast you can apply it to all files, so there is no need to complicate the code and configuration with applying compression selectively. Decompression is also very fast, so it won't slow down your restores.  The price we pay is a compression ratio that isn't too great.

# Encryption

You don't want a cloud storage provider being able to read your backups. Or tamper with them. All backed up files are encrypted, with an AEAD mode/cipher, meaning it is also authenticated, and attempts to modify data are detected.

Your files are protected by a passphrase. Each backed up file starts with a 32 byte salt. For each file, a key is derived using PBKDF2.

# File format

Each backup is made of two files:

1. Data file, containing the contents of all files stored in this backup.
2. Index file, listing all files and meta information in this backup (file name, regular/directory, permissions, mtime, owner/group, and offset into data file. An incremental backup lists all files that would be restored for a restore operation, not only the modified files.

Each file starts with a 32 byte salt. Followed by data in the DARE format (Data at Rest, see https://github.com/minio/sio).

Backups, and the file names are named after the time they were initiated. A backup name has the form YYYYMMDD-hhmmdd. The file names have ".data" and either ".index.full" or ".index.incr" appended.

# License

This software is released under an MIT license. See LICENSE.MD.

# Dependencies

All dependencies are vendored in (include) in this repositories:

	https://github.com/pierrec/lz4 (BSD license)
	https://github.com/minio/sio (Apache license)

# Contact

For feedback, contact Mechiel Lukkien at mechiel@ueber.net.


# Todo

- delete partial backup files on exit
- use temp names for index files when writing, rename to final name after writing.  gives automic backups

- when backing up with verbose, also show how many paths have been backed up, and total file size.
- implement option for setting remote "path" from command-line?  so we can have one config for many directories that we want to backup.
- for restore, on missing/invalid config file, print an example. should make restoring a lot easier in practice.
- for restore, allow specifying paths or regexp on command-line?
- is our behaviour correct when restoring to a directory that already has some files?  we currently fail when we try to create a file/directory that already exists.
