/* =============================================================================
 *  Copyright (c) 2026 Nyx Software, Inc.  All rights reserved.
 *
 *  CodeDistill
 *
 *  Property of Nyx Software, Inc., provided under a dual license: the GNU Affero General
 *  Public License v3.0 (see the LICENSE file) and, separately, a commercial
 *  license available from Nyx Software, Inc. Use outside the terms of one of those
 *  licenses is prohibited.
 *
 *  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Nyx-Commercial
 * ============================================================================= */

import { mount } from 'svelte';
import Root from './Root.svelte';
import PaneWindow from './PaneWindow.svelte';
import { paneWindowParams } from './lib/popout.svelte';
import './app.css';

// A popped-out pane's window (`?pane=jobs`) is the same page showing one pane;
// everything else is the app (docs/design/panes.md, "Popping a pane out").
const target = document.getElementById('app')!;
const pane = paneWindowParams();
// Root asks who is calling before the app loads: sign-in, the access token,
// or the administration view come first where they apply.
const app = pane ? mount(PaneWindow, { target, props: pane }) : mount(Root, { target });
export default app;
