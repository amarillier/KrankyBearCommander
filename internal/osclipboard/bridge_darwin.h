#ifndef KRANKYBEAR_CLIPBOARD_BRIDGE_DARWIN_H
#define KRANKYBEAR_CLIPBOARD_BRIDGE_DARWIN_H

// Writes count file paths (nul-separated, single buffer, with a trailing
// nul after the last path) to the general pasteboard as public.file-url
// items, replacing its current contents. Returns 0 on success, non-zero on
// failure.
int clipboard_write_files(const char *nulSeparatedPaths, int count);

// Reads file paths from the general pasteboard (e.g. after Cmd+C on one or
// more items in Finder). Returns a single nul-separated, nul-terminated
// buffer the caller must release with clipboard_free, or NULL if the
// pasteboard holds no file references. *count is set to the number of
// paths found.
char *clipboard_read_files(int *count);

void clipboard_free(char *p);

#endif
