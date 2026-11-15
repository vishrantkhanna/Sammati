<?php
/**
 * Plugin Name: Sammati Consent Form
 * Description: Adds the [sammati form="newsletter"] shortcode, which shows a consent form from your own Sammati server.
 * Version: 0.1.0
 * License: MIT
 *
 * Install: copy this file to wp-content/plugins/sammati/sammati.php, activate
 * it, then set your server address under Settings > General > Sammati Server.
 * Use: [sammati form="newsletter"] or [sammati form="newsletter" lang="hi"]
 */

if (!defined('ABSPATH')) {
    exit;
}

add_action('admin_init', function () {
    register_setting('general', 'sammati_server', array('type' => 'string', 'sanitize_callback' => 'esc_url_raw'));
    add_settings_field('sammati_server', 'Sammati Server', function () {
        printf('<input type="url" name="sammati_server" value="%s" class="regular-text" placeholder="https://consent.example.com">',
            esc_attr(get_option('sammati_server', '')));
    }, 'general');
});

add_shortcode('sammati', function ($atts) {
    $atts = shortcode_atts(array('form' => '', 'lang' => ''), $atts, 'sammati');
    $server = untrailingslashit(get_option('sammati_server', ''));
    if ($server === '' || !preg_match('/^[a-z][a-z0-9_\-]*$/', $atts['form'])) {
        return current_user_can('manage_options') ? '<p>Sammati: set the server address and a form name.</p>' : '';
    }
    $lang = preg_match('/^[a-z][a-z0-9_\-]*$/', $atts['lang']) ? sprintf(' data-lang="%s"', esc_attr($atts['lang'])) : '';
    $user = wp_get_current_user();
    $email = $user->exists() ? sprintf(' data-identifier="%s"', esc_attr($user->user_email)) : '';
    return sprintf('<script src="%s" data-form="%s"%s%s></script>', esc_url($server . '/widget.js'), esc_attr($atts['form']), $lang, $email);
});
