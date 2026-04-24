# Postgres 16 + PostGIS 3.4 + h3-pg extension.
#
# PostGIS covers geography/spatial. h3-pg adds H3 cell operations in SQL,
# which the app uses for r9 (proximity) and r7 (V2 heatmap aggregation).
# We build h3-pg from source against the Postgres dev headers bundled in
# the postgis base image.

FROM postgis/postgis:16-3.4

# h3-pg upstream is archived read-only as of 2025-12-30 with no release tags,
# so we pin to a specific main-branch commit SHA for reproducibility. Bump
# deliberately by editing the SHA, not by tracking HEAD.
ARG H3_PG_COMMIT=04227cb62a7338eb2a655fff875b125de2f043f4

RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      ca-certificates \
      build-essential \
      cmake \
      git \
      postgresql-server-dev-16 \
 && rm -rf /var/lib/apt/lists/*

RUN git clone https://github.com/zachasme/h3-pg.git /tmp/h3-pg \
 && cd /tmp/h3-pg \
 && git checkout ${H3_PG_COMMIT} \
 && cmake -B build \
 && cmake --build build \
 && cmake --install build \
 && rm -rf /tmp/h3-pg

RUN apt-get purge -y --auto-remove build-essential cmake git postgresql-server-dev-16 \
 && rm -rf /var/lib/apt/lists/*
