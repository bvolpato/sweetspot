#ifndef SWEETSPOT_WIFI_DARWIN_H
#define SWEETSPOT_WIFI_DARWIN_H

#include <stdint.h>

typedef struct {
    char iface[32];
    char ssid[128];
    char bssid[32];
    char country[16];
    int power_on;
    int connected;
    int rssi;
    int noise;
    double tx_rate;
    int channel;
    int band;     // 0=unknown, 1=2.4GHz, 2=5GHz, 3=6GHz
    int width;    // 0=unknown, 1=20MHz, 2=40MHz, 3=80MHz, 4=160MHz
    int phy_mode; // CWPHYMode raw value
    int security; // CWSecurity raw value
} ss_wifi_info_t;

typedef struct {
    char ssid[128];
    char bssid[32];
    int channel;
    int band;
    int width;
    int rssi;
    int noise;
    int security;
} ss_neighbor_t;

int ss_read_wifi_info(ss_wifi_info_t *out);
int ss_scan_neighbors(ss_neighbor_t *out, int max_count);

#endif
