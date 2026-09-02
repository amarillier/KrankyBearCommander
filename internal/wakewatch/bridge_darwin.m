#import <Cocoa/Cocoa.h>
#import "bridge_darwin.h"
#import "_cgo_export.h" // wakewatchOnWake, generated from wakewatch_darwin.go's //export

static id gWakeObserver;

void wakewatch_install(void) {
    @autoreleasepool {
        if (gWakeObserver != nil) {
            return; // already installed
        }
        gWakeObserver = [[[NSWorkspace sharedWorkspace] notificationCenter]
            addObserverForName:NSWorkspaceDidWakeNotification
                        object:nil
                         queue:[NSOperationQueue mainQueue]
                    usingBlock:^(NSNotification *note) {
                        wakewatchOnWake();
                    }];
    }
}
