#import <Cocoa/Cocoa.h>
#import "bridge_darwin.h"
#import "_cgo_export.h" // dragoutHitTest, generated from dragout_darwin.go's //export
#include <stdlib.h>
#include <string.h>
#include <math.h>

@interface DragoutSource : NSObject <NSDraggingSource>
@end

@implementation DragoutSource
- (NSDragOperation)draggingSession:(NSDraggingSession *)session
    sourceOperationMaskForDraggingContext:(NSDraggingContext)context {
    return NSDragOperationCopy;
}
@end

static DragoutSource *gSource;
static NSPoint gMouseDownPoint;
static BOOL gDragging;
static id gMonitor;

void dragout_install(void *nsWindowPtr) {
    @autoreleasepool {
        if (gMonitor != nil) {
            return; // already installed — this app only has one main window
        }
        NSWindow *win = (__bridge NSWindow *)nsWindowPtr;
        NSView *contentView = [win contentView];
        gSource = [[DragoutSource alloc] init];
        gDragging = NO;

        NSEventMask mask = NSEventMaskLeftMouseDown | NSEventMaskLeftMouseDragged | NSEventMaskLeftMouseUp;
        gMonitor = [NSEvent addLocalMonitorForEventsMatchingMask:mask handler:^NSEvent *(NSEvent *event) {
            if (event.window != win) {
                return event; // not our window
            }
            switch (event.type) {
                case NSEventTypeLeftMouseDown:
                    gMouseDownPoint = event.locationInWindow;
                    gDragging = NO;
                    break;
                case NSEventTypeLeftMouseDragged: {
                    if (gDragging) {
                        break;
                    }
                    NSPoint p = event.locationInWindow;
                    CGFloat dx = p.x - gMouseDownPoint.x;
                    CGFloat dy = p.y - gMouseDownPoint.y;
                    if (sqrt(dx * dx + dy * dy) < 4.0) {
                        break;
                    }
                    gDragging = YES;

                    NSPoint viewPoint = [contentView convertPoint:gMouseDownPoint fromView:nil];
                    // Fyne's window-content coordinate convention (matches
                    // Window.SetOnDropped) is top-left-origin; Cocoa view
                    // points are bottom-left-origin unless the view itself
                    // is flipped, so check rather than assume.
                    CGFloat fyneX = viewPoint.x;
                    CGFloat fyneY = contentView.isFlipped
                        ? viewPoint.y
                        : (contentView.bounds.size.height - viewPoint.y);

                    int count = 0;
                    char *joined = dragoutHitTest(fyneX, fyneY, &count);
                    if (joined == NULL || count <= 0) {
                        if (joined != NULL) {
                            free(joined);
                        }
                        break;
                    }

                    NSMutableArray<NSDraggingItem *> *items = [NSMutableArray arrayWithCapacity:count];
                    const char *cp = joined;
                    for (int i = 0; i < count; i++) {
                        NSString *path = [NSString stringWithUTF8String:cp];
                        NSURL *url = [NSURL fileURLWithPath:path];
                        NSDraggingItem *item = [[NSDraggingItem alloc] initWithPasteboardWriter:url];
                        NSImage *icon = [[NSWorkspace sharedWorkspace] iconForFile:path];
                        NSRect frame = NSMakeRect(viewPoint.x - 16 + i * 4, viewPoint.y - 16 - i * 4, 32, 32);
                        [item setDraggingFrame:frame contents:icon];
                        [items addObject:item];
                        cp += strlen(cp) + 1;
                    }
                    free(joined);

                    [contentView beginDraggingSessionWithItems:items event:event source:gSource];
                    break;
                }
                case NSEventTypeLeftMouseUp:
                    gDragging = NO;
                    break;
                default:
                    break;
            }
            return event;
        }];
    }
}
