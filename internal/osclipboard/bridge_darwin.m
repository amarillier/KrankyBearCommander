#import <Cocoa/Cocoa.h>
#import "bridge_darwin.h"
#include <string.h>
#include <stdlib.h>

int clipboard_write_files(const char *nulSeparatedPaths, int count) {
    @autoreleasepool {
        NSMutableArray<NSURL *> *urls = [NSMutableArray arrayWithCapacity:count];
        const char *p = nulSeparatedPaths;
        for (int i = 0; i < count; i++) {
            NSString *path = [NSString stringWithUTF8String:p];
            [urls addObject:[NSURL fileURLWithPath:path]];
            p += strlen(p) + 1;
        }

        NSPasteboard *pb = [NSPasteboard generalPasteboard];
        [pb clearContents];
        BOOL ok = [pb writeObjects:urls];
        return ok ? 0 : 1;
    }
}

char *clipboard_read_files(int *count) {
    @autoreleasepool {
        NSPasteboard *pb = [NSPasteboard generalPasteboard];
        NSDictionary *opts = @{NSPasteboardURLReadingFileURLsOnlyKey: @YES};
        NSArray<NSURL *> *urls = [pb readObjectsForClasses:@[[NSURL class]] options:opts];
        if (urls == nil || urls.count == 0) {
            *count = 0;
            return NULL;
        }

        NSMutableData *buf = [NSMutableData data];
        for (NSURL *url in urls) {
            const char *cpath = [[url path] UTF8String];
            [buf appendBytes:cpath length:strlen(cpath) + 1]; // include the nul terminator
        }

        *count = (int)urls.count;
        char *result = malloc(buf.length);
        memcpy(result, buf.bytes, buf.length);
        return result;
    }
}

void clipboard_free(char *p) {
    free(p);
}
