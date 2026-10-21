//go:build darwin

package nativedialog

/*
#cgo CFLAGS: -x objective-c -fobjc-arc -Wno-deprecated-declarations
#cgo LDFLAGS: -framework Cocoa

#include <stdlib.h>
#include <string.h>

#import <Cocoa/Cocoa.h>
#import <dispatch/dispatch.h>

// modbussim_open_file presents a free-standing NSOpenPanel (not a window
// sheet). Free-standing modal panels can be moved by the user, unlike sheets,
// which macOS anchors to the parent window.
static char* modbussim_open_file(const char* message, const char* extensions) {
    __block char* selected = NULL;
    void (^present)(void) = ^{
        @autoreleasepool {
            NSOpenPanel* panel = [[NSOpenPanel alloc] init];
            [panel setCanChooseFiles:YES];
            [panel setCanChooseDirectories:NO];
            [panel setAllowsMultipleSelection:NO];
            [panel setResolvesAliases:YES];
            if (message != NULL && message[0] != '\0') {
                [panel setMessage:[NSString stringWithUTF8String:message]];
            }

            NSMutableArray<NSString*>* fileTypes = [NSMutableArray array];
            if (extensions != NULL && extensions[0] != '\0') {
                NSString* list = [NSString stringWithUTF8String:extensions];
                for (NSString* ext in [list componentsSeparatedByString:@";"]) {
                    NSString* trimmed = [ext stringByTrimmingCharactersInSet:
                        [NSCharacterSet whitespaceAndNewlineCharacterSet]];
                    if ([trimmed length] > 0) {
                        [fileTypes addObject:trimmed];
                    }
                }
            }
            if ([fileTypes count] > 0) {
                [panel setAllowedFileTypes:fileTypes];
            }

            [NSApp activateIgnoringOtherApps:YES];
            if ([panel runModal] == NSModalResponseOK) {
                NSURL* url = [panel URL];
                const char* path = url != nil ? [[url path] UTF8String] : NULL;
                if (path != NULL) {
                    selected = strdup(path);
                }
            }
        }
    };

    if ([NSThread isMainThread]) {
        present();
    } else {
        dispatch_sync(dispatch_get_main_queue(), present);
    }
    return selected;
}
*/
import "C"

import (
	"context"
	"sync"
	"unsafe"
)

var panelMu sync.Mutex

func openFile(_ context.Context, filters []Filter) (string, error) {
	panelMu.Lock()
	defer panelMu.Unlock()

	cMessage := C.CString(openFileMessage(filters))
	defer C.free(unsafe.Pointer(cMessage))

	cExtensions := C.CString(openFileExtensions(filters))
	defer C.free(unsafe.Pointer(cExtensions))

	result := C.modbussim_open_file(cMessage, cExtensions)
	if result == nil {
		return "", nil
	}
	defer C.free(unsafe.Pointer(result))
	return C.GoString(result), nil
}
