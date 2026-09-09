package pluginsdk

// EventOrganizeFilePreparing is the synchronous hook immediately before the
// host copies one file to its organize target. It carries source and target
// storage/path fields so bounded preparation work (such as playback cache
// warming) can use the local source before a cloud upload starts.
const EventOrganizeFilePreparing = "organize.file.preparing"
