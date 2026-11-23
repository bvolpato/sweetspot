#import <Foundation/Foundation.h>
#import <CoreWLAN/CoreWLAN.h>
#include <string.h>
#include "wifi_darwin.h"

static void copy_nsstring(NSString *src, char *dst, size_t dst_size) {
    if (dst == NULL || dst_size == 0) {
        return;
    }
    dst[0] = '\0';
    if (src == nil) {
        return;
    }
    const char *utf8 = [src UTF8String];
    if (utf8 != NULL) {
        strlcpy(dst, utf8, dst_size);
    }
}

int ss_read_wifi_info(ss_wifi_info_t *out) {
    if (out == NULL) {
        return -1;
    }
    memset(out, 0, sizeof(*out));

    @autoreleasepool {
        CWInterface *iface = [[CWWiFiClient sharedWiFiClient] interface];
        if (iface == nil) {
            return -1;
        }

        copy_nsstring(iface.interfaceName, out->iface, sizeof(out->iface));
        copy_nsstring(iface.ssid, out->ssid, sizeof(out->ssid));
        copy_nsstring(iface.bssid, out->bssid, sizeof(out->bssid));
        copy_nsstring(iface.countryCode, out->country, sizeof(out->country));

        out->power_on = iface.powerOn ? 1 : 0;
        out->rssi = (int)iface.rssiValue;
        out->noise = (int)iface.noiseMeasurement;
        out->tx_rate = iface.transmitRate;
        out->phy_mode = (int)iface.activePHYMode;
        out->security = (int)iface.security;

        CWChannel *ch = iface.wlanChannel;
        if (ch != nil) {
            out->channel = (int)ch.channelNumber;
            out->band = (int)ch.channelBand;
            out->width = (int)ch.channelWidth;
        }

        // On modern macOS without Location Services permission, ssid/bssid can be redacted (nil),
        // while rssi, channel, and txRate remain populated when associated.
        out->connected = (out->power_on && (out->channel > 0 || out->rssi != 0 || out->tx_rate > 0)) ? 1 : 0;
        return 0;
    }
}

int ss_scan_neighbors(ss_neighbor_t *out, int max_count) {
    if (out == NULL || max_count <= 0) {
        return -1;
    }

    @autoreleasepool {
        CWInterface *iface = [[CWWiFiClient sharedWiFiClient] interface];
        if (iface == nil) {
            return -1;
        }

        NSError *err = nil;
        NSSet<CWNetwork *> *networks = [iface scanForNetworksWithName:nil error:&err];
        if (err != nil || networks == nil) {
            return -2;
        }

        int idx = 0;
        for (CWNetwork *net in networks) {
            if (idx >= max_count) {
                break;
            }
            ss_neighbor_t *row = &out[idx];
            memset(row, 0, sizeof(*row));
            copy_nsstring(net.ssid, row->ssid, sizeof(row->ssid));
            copy_nsstring(net.bssid, row->bssid, sizeof(row->bssid));
            row->rssi = (int)net.rssiValue;
            row->noise = (int)net.noiseMeasurement;

            CWChannel *ch = net.wlanChannel;
            if (ch != nil) {
                row->channel = (int)ch.channelNumber;
                row->band = (int)ch.channelBand;
                row->width = (int)ch.channelWidth;
            }

            if ([net supportsSecurity:kCWSecurityWPA3Enterprise]) {
                row->security = kCWSecurityWPA3Enterprise;
            } else if ([net supportsSecurity:kCWSecurityWPA3Personal]) {
                row->security = kCWSecurityWPA3Personal;
            } else if ([net supportsSecurity:kCWSecurityWPA2Enterprise]) {
                row->security = kCWSecurityWPA2Enterprise;
            } else if ([net supportsSecurity:kCWSecurityWPA2Personal]) {
                row->security = kCWSecurityWPA2Personal;
            } else if ([net supportsSecurity:kCWSecurityNone]) {
                row->security = kCWSecurityNone;
            }
            idx++;
        }
        return idx;
    }
}
